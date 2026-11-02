#!/bin/sh
# Install the certwatch CLI from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/titiabdelghafour8/certwatch/main/scripts/install.sh | sh
#
# Environment:
#   CERTWATCH_VERSION      release tag to install (default: latest)
#   CERTWATCH_INSTALL_DIR  target directory (default: /usr/local/bin, or ~/.local/bin if not writable)
set -eu

REPO="titiabdelghafour8/certwatch"
VERSION="${CERTWATCH_VERSION:-latest}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported OS: $os (on Windows use scripts/install.ps1)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
archive="certwatch_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $archive ($VERSION)..."
curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

expected=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "checksum verification failed" >&2
  exit 1
fi

tar -xzf "$tmp/$archive" -C "$tmp" certwatch

dir="${CERTWATCH_INSTALL_DIR:-/usr/local/bin}"
if [ ! -w "$dir" ]; then
  if [ -z "${CERTWATCH_INSTALL_DIR:-}" ]; then
    dir="$HOME/.local/bin"
    mkdir -p "$dir"
  else
    echo "$dir is not writable" >&2
    exit 1
  fi
fi
install -m 0755 "$tmp/certwatch" "$dir/certwatch"

echo "Installed certwatch to $dir/certwatch"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH to use it." ;;
esac
"$dir/certwatch" version
