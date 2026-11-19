#!/bin/sh
# hads installer — macOS · Linux · BSD · WSL
#   curl -fsSL https://fernata-dev.github.io/hads/install.sh | sh
#
# env:
#   HADS_VERSION     pin a release, e.g. v1.0.0 (default: latest)
#   HADS_BIN_DIR     where the binary goes (default: ~/.local/bin)
#   HADS_SETUP_ARGS  passed to `hads setup`, e.g. "--yes --no-service"
#   HADS_DL_URL      download from a mirror instead of GitHub Releases
set -eu

REPO="${HADS_REPO:-fernata-dev/hads}"
if [ -n "${HADS_VERSION:-}" ]; then
  DL="${HADS_DL_URL:-https://github.com/$REPO/releases/download/$HADS_VERSION}"
else
  DL="${HADS_DL_URL:-https://github.com/$REPO/releases/latest/download}"
fi
BIN_DIR="${HADS_BIN_DIR:-$HOME/.local/bin}"

if [ -t 1 ]; then V='\033[38;5;141m'; G='\033[38;5;114m'; R='\033[38;5;203m'; D='\033[2m'; N='\033[0m'; else V=''; G=''; R=''; D=''; N=''; fi
say()  { printf '  %b◆%b %s\n' "$V" "$N" "$*"; }
ok()   { printf '  %b✔%b %s\n' "$G" "$N" "$*"; }
fail() { printf '  %b✘%b %s\n' "$R" "$N" "$*" >&2; exit 1; }

printf '\n  %b◆ hads%b %b· حدس · the terminal that knows what you will type next%b\n\n' "$V" "$N" "$D" "$N"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin|freebsd) ;;
  msys*|mingw*|cygwin*) fail "On Windows open PowerShell and run:  irm https://raw.githubusercontent.com/$REPO/main/scripts/install.ps1 | iex" ;;
  *) fail "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7*|armv6*|armhf) arch=arm ;;
  *) fail "unsupported CPU: $arch" ;;
esac
# Apple Silicon running a Rosetta shell: install the native build
if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
  arch=arm64
fi

dl() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL --retry 2 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else fail "curl or wget is required"; fi
}

sha() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  else sha256 -q "$1"; fi
}

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t hads)
trap 'rm -rf "$tmp"' EXIT INT TERM

name="hads-$os-$arch"
say "downloading $name"
dl "$DL/$name" "$tmp/hads" || fail "download failed: $DL/$name"
dl "$DL/checksums.txt" "$tmp/checksums.txt" || fail "could not fetch checksums"

want=$(awk -v n="$name" '$2==n {print $1}' "$tmp/checksums.txt")
got=$(sha "$tmp/hads")
[ -n "$want" ] && [ "$want" = "$got" ] || fail "checksum mismatch — refusing to install"
ok "sha-256 verified"

mkdir -p "$BIN_DIR"
if [ -x "$BIN_DIR/hads" ]; then "$BIN_DIR/hads" stop >/dev/null 2>&1 || true; fi
cp "$tmp/hads" "$BIN_DIR/hads.new" && chmod 0755 "$BIN_DIR/hads.new" && mv -f "$BIN_DIR/hads.new" "$BIN_DIR/hads"
if [ "$os" = darwin ]; then xattr -d com.apple.quarantine "$BIN_DIR/hads" 2>/dev/null || true; fi
ok "installed $BIN_DIR/hads"

# shellcheck disable=SC2086
"$BIN_DIR/hads" setup ${HADS_SETUP_ARGS:-}
