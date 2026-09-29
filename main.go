// Command sampbot connects N SA-MP bots to a server for a fixed duration to
// load-test it, then exits. It is a standalone tool derived from the gosamp
// soak-test harness.
//
// Usage:
//
//	./run.sh IP:PORT BOT_COUNT DURATION_SECONDS
//	./run.sh 213.163.195.29:7777 50 3600
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"sampbot/bot"
)

const (
	maxBots      = 1000
	chatInterval = 2 * time.Second
	reportEvery  = 10 * time.Second
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: sampbot IP:PORT BOT_COUNT DURATION_SECONDS")
		fmt.Fprintln(os.Stderr, "example: sampbot 213.163.195.29:7777 50 3600")
		os.Exit(2)
	}
	addr := os.Args[1]
	botsArg := os.Args[2]
	durArg := os.Args[3]

	srv, err := parseTarget(addr)
	if err != nil {
		fatalf("%v", err)
	}
	n, err := parseBotCount(botsArg)
	if err != nil {
		fatalf("%v", err)
	}
	dur, err := parseDuration(durArg)
	if err != nil {
		fatalf("%v", err)
	}

	if err := run(srv, n, dur); err != nil {
		fatalf("%v", err)
	}
}

// parseTarget validates "IP:PORT" (host may be a name).
func parseTarget(s string) (*net.UDPAddr, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return nil, fmt.Errorf("invalid IP:PORT %q: %v", s, err)
	}
	if host == "" {
		return nil, fmt.Errorf("invalid IP:PORT %q: empty host", s)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port %q: must be 1..65535", portStr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Allow a resolvable hostname too.
		if _, rerr := net.ResolveIPAddr("ip4", host); rerr != nil {
			return nil, fmt.Errorf("invalid host %q", host)
		}
	}
	udp, err := net.ResolveUDPAddr("udp4", s)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %v", s, err)
	}
	return udp, nil
}

func parseBotCount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid bot count %q: not a number", s)
	}
	if n < 1 {
		return 0, fmt.Errorf("invalid bot count %d: must be positive", n)
	}
	if n > maxBots {
		return 0, fmt.Errorf("invalid bot count %d: exceeds the safe maximum of %d", n, maxBots)
	}
	return n, nil
}

func parseDuration(s string) (time.Duration, error) {
	sec, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: not a number of seconds", s)
	}
	if sec < 1 {
		return 0, fmt.Errorf("invalid duration %d: must be positive seconds", sec)
	}
	return time.Duration(sec) * time.Second, nil
}

func run(srv *net.UDPAddr, n int, dur time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()

	// Ctrl-C stops early and still prints a summary.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\ninterrupted, shutting down bots...")
		cancel()
	}()

	fmt.Printf("sampbot: connecting %d bots to %s for %s\n", n, srv.String(), dur)

	var connected, failed, survived int64
	start := time.Now()
	results := make([]bot.Result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("Bot%03d", idx)
			cl, err := bot.New(ctx, srv)
			if err != nil {
				results[idx] = bot.Result{Index: idx, Name: name, Err: err}
				atomic.AddInt64(&failed, 1)
				return
			}
			r := cl.Run(ctx, name, chatInterval, func() { atomic.AddInt64(&connected, 1) })
			cl.Close()
			if r.Err != nil {
				atomic.AddInt64(&failed, 1)
			} else if r.Connected {
				atomic.AddInt64(&survived, 1)
			}
			results[idx] = r
		}(i)
	}

	// Periodic status line.
	ticker := time.NewTicker(reportEvery)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				remaining := dur - time.Since(start)
				if remaining < 0 {
					remaining = 0
				}
				fmt.Printf("[%s] connected=%d failed=%d remaining=%s\n",
					time.Since(start).Round(time.Second),
					atomic.LoadInt64(&connected), atomic.LoadInt64(&failed),
					remaining.Round(time.Second))
			}
		}
	}()

	wg.Wait()
	actual := time.Since(start)

	// Summary.
	var ok, dropped int
	var reasons = map[string]int{}
	for _, r := range results {
		if r.Err != nil {
			dropped++
			if r.Err != nil {
				reasons[r.Err.Error()]++
			}
		} else if r.Connected {
			ok++
		}
	}
	fmt.Println("----- summary -----")
	fmt.Printf("bots requested : %d\n", n)
	fmt.Printf("survived       : %d\n", ok)
	fmt.Printf("disconnected   : %d\n", dropped)
	fmt.Printf("actual duration: %s\n", actual.Round(time.Second))
	if len(reasons) > 0 {
		fmt.Println("failure reasons:")
		for msg, c := range reasons {
			fmt.Printf("  %dx %s\n", c, msg)
		}
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sampbot: "+format+"\n", args...)
	os.Exit(1)
}
