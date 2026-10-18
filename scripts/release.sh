#!/usr/bin/env bash
# Build portable CLI archives into DIR (default dist) on a Linux amd64
# runner. Archive names carry no version, so links to
# releases/latest/download/... keep working from one release to the next.
set -euo pipefail
cd "$(dirname "$0")/.."
version=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/cli/cli.go)
[[ -n "$version" ]] || { echo 'internal/cli/cli.go has no const Version' >&2; exit 1; }
out=${1:-dist}
[[ ! -e "$out" ]] || { echo 'Use a new output directory' >&2; exit 1; }
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    name="forgeschema_${os}_${arch}"
    folder="$work/$name"
    mkdir -p "$folder"
    binary=forgeschema
    [[ "$os" != windows ]] || binary=forgeschema.exe
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$folder/$binary" ./cmd/forgeschema
    cp LICENSE NOTICE THIRD_PARTY_NOTICES.md README.md COMMANDS.md FEATURES.md SCHEMA.md PLANNING.md APPLY.md VERIFY.md "$folder/"
    if [[ "$os/$arch" == linux/amd64 ]]; then
      sh scripts/smoke.sh "$folder/$binary" "$version"
    fi
    if [[ "$os" == windows ]]; then
      (cd "$work" && zip -qr "$out/$name.zip" "$name")
    else
      tar -czf "$out/$name.tar.gz" -C "$work" "$name"
    fi
  done
done
(cd "$out" && sha256sum ./*.tar.gz ./*.zip | sed 's| \./| |' > SHA256SUMS && sha256sum --check SHA256SUMS)
echo "Built six CLI archives and verified SHA256SUMS for $version"
