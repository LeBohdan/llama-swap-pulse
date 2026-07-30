#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

echo "Building llama-swap-pulse..."
go build -o llama-swap-pulse ./cmd

echo "Linting..."
go vet ./...

echo "Done: ./llama-swap-pulse"
