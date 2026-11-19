#!/usr/bin/env bash
# Cross-compile hads for every platform into dist/ with SHA-256 checksums.
#   ./scripts/build.sh                 # VERSION defaults to `git describe` or today's date
#   VERSION=1.2.0 ./scripts/build.sh
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null | sed 's/^v//' || date +%Y.%m.%d)}"
OUT="${OUT:-dist}"
TARGETS=(
  linux/amd64 linux/arm64 linux/arm
  darwin/amd64 darwin/arm64
  windows/amd64 windows/arm64
  freebsd/amd64
)

rm -rf "$OUT" && mkdir -p "$OUT"
for t in "${TARGETS[@]}"; do
  os=${t%/*} arch=${t#*/} ext=""
  [[ $os == windows ]] && ext=.exe
  printf '  %-16s' "$os/$arch"
  GOOS=$os GOARCH=$arch GOARM=7 CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/hads-$os-$arch$ext" ./cmd/hads
  du -h "$OUT/hads-$os-$arch$ext" | cut -f1
done
(
  cd "$OUT"
  if command -v sha256sum >/dev/null; then sha256sum hads-*; else shasum -a 256 hads-*; fi > checksums.txt
)
echo "  version $VERSION → $OUT/"
