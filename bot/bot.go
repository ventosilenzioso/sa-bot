// Package bot implements a minimal SA-MP client used for load testing: it
// performs the full RakNet legacy handshake, joins, picks a class, spawns, and
// sends periodic chat to stay connected. It is derived from the proven
// gosamp-bench/gosamp-client harness.
//
// It is strictly a load-testing client: it only sends the packets a normal
// client sends (handshake, class/spawn, and occasional chat). No flooding, no
// exploits.
package bot

import (
	"context"
	"fmt"
	"net"
	"time"

	"sampbot/internal/protocol"
	"sampbot/internal/raknet"
)

// Result is the outcome of one bot for the whole run.
type Result struct {
	Index     int
	Name      string
	Connected bool // reached spawn
	Err       error
	Chats     int
}

// Client is one bot connection.
type Client struct {
	pc   net.PacketConn
	srv  *net.UDPAddr
	port uint16
	conn *raknet.Conn
	ctx  context.Context
}

// New creates a bot bound to an ephemeral local UDP port targeting srv.
func New(ctx context.Context, srv *net.UDPAddr) (*Client, error) {
	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	la := pc.LocalAddr().(*net.UDPAddr)
	return &Client{
		pc:   pc,
		srv:  srv,
		port: uint16(la.Port),
		ctx:  ctx,
		conn: raknet.NewConn(raknet.Config{MessagesLimit: 100000, AcksLimit: 100000}, time.Now()),
	}, nil
}

// Close releases the socket.
func (c *Client) Close() { _ = c.pc.Close() }

// Run performs the full lifecycle for this bot until ctx is done: connect,
// spawn, then periodic chat. name must be a unique nickname. onSpawn, if
// non-nil, is called once immediately after the bot spawns successfully.
func (c *Client) Run(ctx context.Context, name string, chatEvery time.Duration, onSpawn func()) Result {
	res := Result{Name: name}
	if err := c.handshake(name); err != nil {
		res.Err = err
		return res
	}
	res.Connected = true
	if onSpawn != nil {
		onSpawn()
	}

	// Keep the link alive with periodic chat, flushing ACKs/retransmits in
	// between. This mirrors a normal idle-then-chat client.
	t := time.NewTicker(chatEvery)
	defer t.Stop()
	i := 0
	for {
		select {
		case <-ctx.Done():
			c.disconnect()
			return res
		case <-t.C:
			i++
			if err := c.sendChat(fmt.Sprintf("loadtest %s %d", name, i)); err == nil {
				res.Chats++
			}
		}
	}
}

// handshake runs cookie -> connection request -> auth -> NIC -> ClientJoin ->
// class -> spawn.
func (c *Client) handshake(name string) error {
	// 1. Cookie handshake.
	var cookie uint16
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := c.pc.WriteTo(raknet.EncryptDatagram(raknet.BuildOpenRequest(cookie), uint16(c.srv.Port)), c.srv); err != nil {
			return err
		}
		raw, err := c.recvRaw(2 * time.Second)
		if err != nil {
			return fmt.Errorf("cookie reply: %w", err)
		}
		off := raknet.ParseOffline(raw)
		switch off.Kind {
		case raknet.OfflineCookieReply:
			cookie = off.Cookie
		case raknet.OfflineOpenReply:
			goto opened
		default:
			return fmt.Errorf("unexpected offline reply %x", raw)
		}
		cookie ^= raknet.SAMPPetarded
	}
	return fmt.Errorf("cookie handshake failed")
opened:

	// 2. Connection request + auth.
	if err := c.enqueue([]byte{raknet.IDConnectionRequest}, raknet.Reliable); err != nil {
		return err
	}
	challenge, err := c.waitRaw(raknet.IDAuthKey, 5*time.Second)
	if err != nil {
		return err
	}
	chLen := int(challenge[1])
	entry, _, ok := protocol.FindAuthBySend(string(challenge[2 : 2+chLen-1]))
	if !ok {
		return fmt.Errorf("unknown auth challenge")
	}
	if err := c.enqueue(append([]byte{raknet.IDAuthKey, 40}, []byte(entry.Recv)...), raknet.Reliable); err != nil {
		return err
	}
	cra, err := c.waitRaw(raknet.IDConnectionRequestAccepted, 5*time.Second)
	if err != nil {
		return err
	}
	if len(cra) != 13 {
		return fmt.Errorf("short connection-accepted (%d bytes)", len(cra))
	}
	token := uint32(cra[9]) | uint32(cra[10])<<8 | uint32(cra[11])<<16 | uint32(cra[12])<<24

	// 3. New incoming connection.
	if err := c.enqueue([]byte{raknet.IDNewIncomingConnection, 127, 0, 0, 1, byte(c.port), byte(c.port >> 8)}, raknet.Reliable); err != nil {
		return err
	}
	if _, err := c.exchangeUntil(nil, time.Now().Add(2*time.Millisecond)); err != nil {
		return err
	}

	// 4. ClientJoin.
	if err := c.enqueue(protocol.BuildRPCFrame(protocol.RPCClientJoin, buildJoin(name, token)), raknet.Reliable); err != nil {
		return err
	}
	if _, err := c.waitRPC(protocol.RPCInitGame, 5*time.Second); err != nil {
		return err
	}

	// 5. Class -> spawn.
	if err := c.enqueue(protocol.BuildRPCFrame(protocol.RPCRequestClass, []byte{0, 0, 0, 0}), raknet.Reliable); err != nil {
		return err
	}
	classResp, err := c.waitRPC(protocol.RPCRequestClass, 5*time.Second)
	if err != nil {
		return err
	}
	if _, err := protocol.ParseSpawnInfo(classResp[1:]); err != nil {
		return err
	}
	if err := c.enqueue(protocol.BuildRPCFrame(protocol.RPCRequestSpawn, nil), raknet.Reliable); err != nil {
		return err
	}
	allow, err := c.waitRPC(protocol.RPCRequestSpawn, 5*time.Second)
	if err != nil {
		return err
	}
	if len(allow) != 4 || allow[0] != 1 {
		return fmt.Errorf("spawn not allowed: %x", allow)
	}
	if err := c.enqueue(protocol.BuildRPCFrame(protocol.RPCSpawn, nil), raknet.Reliable); err != nil {
		return err
	}
	if _, err := c.exchangeUntil(nil, time.Now().Add(2*time.Millisecond)); err != nil {
		return err
	}
	return nil
}

// sendChat sends one chat line (RPC 101) and flushes it.
func (c *Client) sendChat(text string) error {
	payload := append([]byte{byte(len(text))}, []byte(text)...)
	if err := c.enqueue(protocol.BuildRPCFrame(protocol.RPCChat, payload), raknet.Reliable); err != nil {
		return err
	}
	return c.flush()
}

// disconnect sends the clean-quit notification (best effort).
func (c *Client) disconnect() {
	_ = c.enqueue([]byte{raknet.IDDisconnectionNotification}, raknet.Reliable)
	_, _ = c.exchangeUntil(nil, time.Now().Add(50*time.Millisecond))
}

func (c *Client) enqueue(payload []byte, rel byte) error {
	return c.conn.Enqueue(payload, rel, 0)
}

// flush sends whatever the reliability layer currently has queued.
func (c *Client) flush() error {
	for _, f := range c.conn.Tick(time.Now()) {
		if _, err := c.pc.WriteTo(raknet.EncryptDatagram(f, uint16(c.srv.Port)), c.srv); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) recvRaw(d time.Duration) ([]byte, error) {
	_ = c.pc.SetReadDeadline(time.Now().Add(d))
	buf := make([]byte, 2048)
	for {
		n, addr, err := c.pc.ReadFrom(buf)
		if err != nil {
			return nil, err
		}
		if addr.String() != c.srv.String() {
			continue
		}
		return append([]byte(nil), buf[:n]...), nil
	}
}

func (c *Client) decode(raw []byte) [][]byte {
	if len(raw) <= 3 {
		if off := raknet.ParseOffline(raw); off.Kind != raknet.OfflineOther {
			return [][]byte{raw}
		}
	}
	got, err := c.conn.HandleDatagram(raw, time.Now())
	if err != nil {
		return nil
	}
	var out [][]byte
	for _, p := range got {
		nb := (p.DataBitLength + 7) / 8
		out = append(out, append([]byte(nil), p.Data[:nb]...))
	}
	return out
}

// exchangeUntil flushes once, then reads until match(all) is true or the
// deadline elapses. match may be nil (read until deadline). It also re-flushes
// the reliability queue periodically so ACKs/retransmits keep the session
// alive without spamming (30 ms throttle).
func (c *Client) exchangeUntil(match func([][]byte) bool, deadline time.Time) ([][]byte, error) {
	var all [][]byte
	c.flush()
	lastSend := time.Now()
	buf := make([]byte, 2048)
	for {
		select {
		case <-c.ctx.Done():
			return all, c.ctx.Err()
		default:
		}
		if time.Since(lastSend) >= 30*time.Millisecond {
			_ = c.flush()
			lastSend = time.Now()
		}
		next := time.Now().Add(5 * time.Millisecond)
		if next.After(deadline) {
			next = deadline
		}
		_ = c.pc.SetReadDeadline(next)
		n, addr, err := c.pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if !time.Now().Before(deadline) {
					return all, nil
				}
				continue
			}
			return all, err
		}
		if addr.String() != c.srv.String() {
			continue
		}
		all = append(all, c.decode(buf[:n])...)
		if match != nil && match(all) {
			return all, nil
		}
	}
}

func (c *Client) waitRaw(id byte, d time.Duration) ([]byte, error) {
	var found []byte
	_, err := c.exchangeUntil(func(all [][]byte) bool {
		for _, p := range all {
			if len(p) > 0 && p[0] == id {
				found = p
				return true
			}
		}
		return false
	}, time.Now().Add(d))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("timeout waiting for packet %d", id)
	}
	return found, nil
}

func (c *Client) waitRPC(id byte, d time.Duration) ([]byte, error) {
	var found []byte
	_, err := c.exchangeUntil(func(all [][]byte) bool {
		for _, p := range all {
			if rid, body, err := protocol.ParseRPCFrame(p); err == nil && rid == id {
				found = body
				return true
			}
		}
		return false
	}, time.Now().Add(d))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("timeout waiting for RPC %d", id)
	}
	return found, nil
}

// buildJoin builds the ClientJoin (RPC 25) payload for the 0.3.7 client
// version.
func buildJoin(name string, token uint32) []byte {
	bs := raknet.New()
	bs.WriteUint32(protocol.ClientVersion037)
	bs.WriteUint8(1)
	bs.WriteUint8(byte(len(name)))
	bs.WriteAlignedBytes([]byte(name))
	bs.WriteUint32(token ^ protocol.ClientVersion037)
	bs.WriteUint8(3)
	bs.WriteAlignedBytes([]byte("3E9"))
	bs.WriteUint8(5)
	bs.WriteAlignedBytes([]byte("0.3.7"))
	return bs.Bytes()[:bs.Len()]
}
