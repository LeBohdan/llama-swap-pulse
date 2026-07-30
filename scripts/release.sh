#!/usr/bin/env bash
#
# Build binaries for all platforms and publish a GitHub release.
#
# The version is read from internal/version/version.go. Edit that file
# before running the script.
#
# Usage:
#   scripts/release.sh
#
# Requires: gh (authenticated via `gh auth login`).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BINARY="llama-swap-pulse"
VERSION_FILE="internal/version/version.go"

# --- read version from internal/version/version.go ---
VERSION="$(sed -n 's/.*Version = "\(.*\)".*/\1/p' "$VERSION_FILE")"
if [ -z "$VERSION" ]; then
  echo "ERROR: could not read Version from $VERSION_FILE" >&2
  exit 1
fi

TAG="v${VERSION}"
echo "Version: $VERSION (tag: $TAG)"

# --- refuse to clobber an existing release ---
if gh release view "$TAG" >/dev/null 2>&1; then
  echo "ERROR: release $TAG already exists on GitHub" >&2
  exit 1
fi

# --- define platforms ---
PLATFORMS=("linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64" "windows/amd64")

# --- build ---
echo "Building..."
DIST_DIR="${ROOT}/dist/${TAG}"
mkdir -p "$DIST_DIR"

for PLATFORM in "${PLATFORMS[@]}"; do
  OS="${PLATFORM%%/*}"
  ARCH="${PLATFORM##*/}"
  OUT="${DIST_DIR}/${BINARY}-${OS}-${ARCH}"

  if [ "$OS" = "windows" ]; then
    OUT="${OUT}.exe"
  fi

  echo "  $OS/$ARCH"
  GOOS="$OS" GOARCH="$ARCH" go build -o "$OUT" ./cmd
done

# --- create tarballs and copy deploy files ---
for PLATFORM in "${PLATFORMS[@]}"; do
  OS="${PLATFORM%%/*}"
  ARCH="${PLATFORM##*/}"
  TAR="${DIST_DIR}/${BINARY}-${OS}-${ARCH}.tar.gz"

  echo "  packing $OS/$ARCH"
  mkdir -p "${DIST_DIR}/pkg-${OS}-${ARCH}"

  SRC="${DIST_DIR}/${BINARY}-${OS}-${ARCH}"
  [ "$OS" = "windows" ] && SRC="${SRC}.exe"
  cp "$SRC" "${DIST_DIR}/pkg-${OS}-${ARCH}/"
  cp -r "${ROOT}/deploy" "${DIST_DIR}/pkg-${OS}-${ARCH}/"

  tar -C "${DIST_DIR}/pkg-${OS}-${ARCH}" -czf "$TAR" .
  rm -rf "${DIST_DIR}/pkg-${OS}-${ARCH}"
done

# --- create release ---
echo "Creating GitHub release $TAG..."
ASSETS=()
for PLATFORM in "${PLATFORMS[@]}"; do
  OS="${PLATFORM%%/*}"
  ARCH="${PLATFORM##*/}"
  ASSETS+=("${DIST_DIR}/${BINARY}-${OS}-${ARCH}.tar.gz")
done

gh release create "$TAG" "${ASSETS[@]}" \
  --target "$(git rev-parse HEAD)" \
  --generate-notes

echo "Done: published release $TAG"

</content>, 