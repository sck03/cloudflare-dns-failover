#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist
version=${VERSION:-$(git describe --tags --always --dirty)}
export CGO_ENABLED=0
for target in linux/amd64 linux/arm64 linux/arm/v7 windows/amd64; do
  IFS=/ read -r GOOS GOARCH variant <<< "$target"
  export GOOS GOARCH
  export GOARM=${variant#v}
  name="cfguard-${GOOS}-${GOARCH}${variant:+v${GOARM}}"
  [[ $GOOS != windows ]] || name+='.exe'
  go build -trimpath -ldflags="-s -w -X main.version=$version" -o "dist/$name" .
done
cp config.example.yaml dist/
(cd dist && sha256sum cfguard-* > SHA256SUMS)
