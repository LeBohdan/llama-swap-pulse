#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

mkdir -p dist

echo "Building llama-swap-pulse..."
go build -o dist/llama-swap-pulse ./cmd

echo "Linting..."
go vet ./...

echo "Done: ./dist/llama-swap-pulse"
