#!/bin/sh
# Install dothaven on a machine that has nothing yet — no Homebrew, no Go.
#
#   curl -fsSL https://raw.githubusercontent.com/doguyilmaz/dothaven/main/scripts/install.sh | sh
#
# A freshly wiped Mac is exactly where dothaven is needed first, and exactly
# where `brew install` is slowest: Homebrew itself needs the Xcode command-line
# tools before it can install anything. This fetches the release binary for
# this OS and CPU, checks it against the release's published SHA-256 sums, and
# puts it in ~/.local/bin (no sudo). The macOS binaries are signed and
# notarized.
#
# Environment:
#   DOTHAVEN_VERSION   a tag such as v1.4.0 (default: the latest release)
#   DOTHAVEN_BIN_DIR   where to install (default: ~/.local/bin)
#   DOTHAVEN_BASE_URL  download base, for mirrors and tests
#                      (default: https://github.com/doguyilmaz/dothaven/releases)
set -eu

say() { printf '%s\n' "$*"; }
die() { printf 'dothaven install: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) die "unsupported OS: $os (dothaven runs on macOS and Linux)" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported CPU: $arch" ;;
esac

base=${DOTHAVEN_BASE_URL:-https://github.com/doguyilmaz/dothaven/releases}
version=${DOTHAVEN_VERSION:-latest}
if [ "$version" = latest ]; then
  url="$base/latest/download"
else
  url="$base/download/$version"
fi
bin_dir=${DOTHAVEN_BIN_DIR:-$HOME/.local/bin}
asset="dothaven_${os}_${arch}.tar.gz"

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 --connect-timeout 15 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -T 30 -O "$2" "$1"; }
else
  die "needs curl or wget"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "needs sha256sum or shasum to verify the download"
fi

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t dothaven)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading $asset ($version)…"
fetch "$url/$asset" "$tmp/$asset" || die "could not download $url/$asset"
fetch "$url/checksums.txt" "$tmp/checksums.txt" || die "could not download the checksums"

want=$(awk -v f="$asset" '$2 == f || $2 == "*"f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$asset is not listed in checksums.txt"
got=$(sha "$tmp/$asset")
[ "$want" = "$got" ] || die "checksum mismatch for $asset (expected $want, got $got) — not installing"

tar -xzf "$tmp/$asset" -C "$tmp" dothaven || die "the archive has no dothaven binary"
mkdir -p "$bin_dir"
install -m 0755 "$tmp/dothaven" "$bin_dir/dothaven" 2>/dev/null || {
  cp "$tmp/dothaven" "$bin_dir/dothaven" && chmod 0755 "$bin_dir/dothaven"
}

say "Installed dothaven to $bin_dir/dothaven (checksum verified)."
case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) say ""
     say "$bin_dir is not on your PATH yet. For this shell:"
     say "  export PATH=\"$bin_dir:\$PATH\""
     say "and add that line to your ~/.zshrc (or ~/.bashrc) — a restored one may already have it." ;;
esac
say ""
say "Next: run  dothaven  for the menu, or  dothaven restore  to bring a backup back."
