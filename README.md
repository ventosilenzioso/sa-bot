# sampbot

Standalone SA-MP load-testing bot tool. It connects N bots to any SA-MP
0.3.7 server, keeps them alive with periodic chat, and reports connect/spawn
counts and a final summary.

Derived from the proven soak-test harness in the `gosamp` project, simplified
into a single command-line tool.

## Build & run

```bash
./run.sh IP:PORT BOT_COUNT DURATION_SECONDS
```

Examples:

```bash
./run.sh 213.163.195.29:7777 50 3600     # 50 bots for 1 hour
./run.sh 127.0.0.1:7777 20 60            # 20 bots for 1 minute
```

`run.sh` builds the binary automatically if it is missing or the Go sources
changed, then runs it with your arguments.

## Arguments

| Arg | Meaning |
|---|---|
| `IP:PORT` | Target server (hostname also accepted). Port 1-65535. |
| `BOT_COUNT` | Number of concurrent bots, 1-1000. |
| `DURATION_SECONDS` | How long to run, in seconds. |

Invalid input prints a clear error and exits non-zero.

## Output

Every 10 seconds:

```
[10s] connected=50 failed=0 remaining=59m50s
```

At the end:

```
----- summary -----
bots requested : 50
survived       : 50
disconnected   : 0
actual duration: 1h0m0s
```

## Behavior

Each bot runs the full client lifecycle in its own goroutine:

1. RakNet legacy handshake (offline open request, cookie challenge, auth
   challenge, connection accepted, new incoming connection).
2. `ClientJoin` (RPC 25).
3. Class selection (RPC 128) and spawn (RPC 129 / 52).
4. Periodic chat (RPC 101) every 2 seconds to look like an active client.
5. ACK/NACK + resend handled by the reliability layer so the session does not
   time out. The reader uses `exchangeUntil(match)` (stop as soon as the
   awaited reply arrives) with no artificial fixed read deadline.

Bots use unique nicknames `Bot000`, `Bot001`, ... and disconnect cleanly when
the duration ends.
