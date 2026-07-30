#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$SCRIPT_DIR/../dist/llama-swap-pulse"

if [ ! -f "$BINARY" ]; then
  echo "Error: binary not found at $BINARY"
  echo "Build first: ./build.sh"
  exit 1
fi

# Default values — change here, don't pass every time
export LLAMA_SWAP_URL="http://localhost:8001"
export SERVER_LISTEN=":8090"

exec "$BINARY" "${@:-}"