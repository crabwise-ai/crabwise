#!/bin/sh
set -eu

REPO="crabwise-ai/crabwise"
BINARY="crabwise"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux) ;;
  darwin) ;;
  *) echo "error: unsupported OS: $OS" >&2; exit 1 ;;
esac

# detect arch
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

# resolve version
if [ -n "${VERSION:-}" ]; then
  TAG="v${VERSION#v}"
else
  # /releases/latest skips pre-releases, so query all and take the first
  TAG="$(curl -sSf "https://api.github.com/repos/$REPO/releases?per_page=1" | grep '"tag_name"' | head -1 | cut -d'"' -f4)"
  if [ -z "$TAG" ]; then
    echo "error: could not determine latest version" >&2
    exit 1
  fi
fi

VERSION="${TAG#v}"
ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$TAG/$ARCHIVE"

echo "installing $BINARY $TAG ($OS/$ARCH)..."

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

curl -sSfL "$URL" -o "$TMPDIR/$ARCHIVE"

# checksum verification (fail-closed)
CHECKSUMS_URL="https://github.com/$REPO/releases/download/$TAG/checksums.txt"
if ! curl -sSfL "$CHECKSUMS_URL" -o "$TMPDIR/checksums.txt"; then
  echo "error: could not download checksums.txt" >&2
  exit 1
fi

EXPECTED=$(grep -F "$ARCHIVE" "$TMPDIR/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED" ]; then
  echo "error: checksum not found for $ARCHIVE in checksums.txt" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMPDIR/$ARCHIVE" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "$TMPDIR/$ARCHIVE" | awk '{print $1}')
else
  echo "error: no sha256 tool found, cannot verify checksum" >&2
  exit 1
fi

if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "error: checksum mismatch for $ARCHIVE" >&2
  echo "  expected: $EXPECTED" >&2
  echo "  actual:   $ACTUAL" >&2
  exit 1
fi
echo "checksum verified."

tar xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR"

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY"
else
  echo "installing to $INSTALL_DIR (requires sudo)..."
  sudo mv "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY"
fi

chmod +x "$INSTALL_DIR/$BINARY"
echo "installed $BINARY $TAG to $INSTALL_DIR/$BINARY"
