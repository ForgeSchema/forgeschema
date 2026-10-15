#!/usr/bin/env bash
# Build portable CLI archives; execute on a Linux amd64 release runner.
set -euo pipefail
cd "$(dirname "$0")/.."
version=$(cat VERSION)
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ ]] || { echo 'Expected an explicit alpha VERSION' >&2; exit 1; }
grep -Fq "const Version = \"$version\"" internal/cli/cli.go
out=${1:-dist}
[[ ! -e "$out" ]] || { echo 'Use a new output directory' >&2; exit 1; }
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    name="forgeschema_${version}_${os}_${arch}"
    folder="$work/$name"
    mkdir -p "$folder"
    binary=forgeschema
    [[ "$os" != windows ]] || binary=forgeschema.exe
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$folder/$binary" ./cmd/forgeschema
    cp LICENSE NOTICE THIRD_PARTY_NOTICES.md README.md COMMANDS.md FEATURES.md APPLY.md VERIFY.md "$folder/"
    if [[ "$os/$arch" == linux/amd64 ]]; then
      [[ "$("$folder/$binary" version)" == "$version" ]]
      "$folder/$binary" validate examples/customers.yaml >/dev/null
      "$folder/$binary" plan examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml > "$work/plan.json"
      cmp "$work/plan.json" examples/customer-plan.json
    fi
    if [[ "$os" == windows ]]; then
      (cd "$work" && zip -qr "$out/$name.zip" "$name")
    else
      tar -czf "$out/$name.tar.gz" -C "$work" "$name"
    fi
  done
done
(cd "$out" && sha256sum ./*.tar.gz ./*.zip > SHA256SUMS && sha256sum --check SHA256SUMS)
echo "Built six CLI archives and verified SHA256SUMS for $version"
