#!/usr/bin/env bash
# sampbot runner: builds the binary if missing or stale, then runs it with the
# arguments passed through unchanged.
#
# Usage:
#   ./run.sh IP:PORT BOT_COUNT DURATION_SECONDS
#   ./run.sh 213.163.195.29:7777 50 3600
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/sampbot"

needs_build=0
if [ ! -x "$BIN" ]; then
    needs_build=1
elif [ -n "$(find "$DIR" -name '*.go' -newer "$BIN" -print -quit)" ]; then
    needs_build=1
fi

if [ "$needs_build" -eq 1 ]; then
    echo "sampbot: building..." >&2
    (cd "$DIR" && go build -o "$BIN" .)
fi

exec "$BIN" "$@"
