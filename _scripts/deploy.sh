#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

HOST="${DEPLOY_HOST:-polaris}"
USER="${DEPLOY_USER:-llama}"
TARGET_PATH="${DEPLOY_PATH:-/home/llama/llama-swap}"
REMOTE="${USER}@${HOST}"

cd "$PROJECT_DIR"

echo "Building llama-swap-pulse..."
./build.sh

echo "Uploading to ${REMOTE}:${TARGET_PATH}/..."

BATCH_FILE=$(mktemp)
trap 'rm -f "$BATCH_FILE"' EXIT

cat > "$BATCH_FILE" <<EOF
cd "$TARGET_PATH"
put llama-swap-pulse llama-swap-pulse
chmod +x llama-swap-pulse
EOF

sftp -b "$BATCH_FILE" "$REMOTE"

echo "Deployed successfully to ${REMOTE}:${TARGET_PATH}/llama-swap-pulse"
