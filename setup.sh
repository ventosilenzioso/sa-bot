#!/usr/bin/env bash
# One-shot setup for sampbot on a fresh Debian/Ubuntu shell (e.g. Google Cloud
# Shell). Installs Go if missing, clones the repo, and builds the binary.
#
# Usage:
#   bash <(curl -fsSL https://raw.githubusercontent.com/ventosilenzioso/sa-bot/main/setup.sh)
#   # then:
#   cd sa-bot && ./run.sh IP:PORT BOT_COUNT DURATION_SECONDS
set -euo pipefail

REPO_URL="https://github.com/ventosilenzioso/sa-bot.git"
DIR="sa-bot"

echo "== sampbot setup =="

# 1. Go (Cloud Shell usually ships it; install if missing).
if ! command -v go >/dev/null 2>&1; then
    echo "-- Go not found, installing to /usr/local/go ..."
    GO_VERSION="1.24.5"
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64) GOARCH=amd64 ;;
        aarch64|arm64) GOARCH=arm64 ;;
        *) echo "unsupported arch $ARCH"; exit 1 ;;
    esac
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" -o /tmp/go.tgz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf /tmp/go.tgz
    export PATH="/usr/local/go/bin:$PATH"
    echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$HOME/.bashrc"
else
    echo "-- Go: $(go version)"
fi

# 2. Fetch the source.
if [ -d "$DIR/.git" ]; then
    echo "-- updating existing $DIR"
    git -C "$DIR" pull --ff-only
else
    echo "-- cloning $REPO_URL"
    git clone --depth 1 "$REPO_URL" "$DIR"
fi

# 3. Build.
echo "-- building"
(cd "$DIR" && go build -o sampbot .)
echo
echo "== done =="
echo "run:  cd $DIR && ./run.sh IP:PORT BOT_COUNT DURATION_SECONDS"
echo "try:  cd $DIR && ./run.sh 127.0.0.1:7777 20 60"
