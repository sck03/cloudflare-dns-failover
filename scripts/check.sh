#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

unformatted=$(git ls-files -z '*.go' | xargs -0 gofmt -l)
if [[ -n "$unformatted" ]]; then
  echo "Run gofmt on:"
  echo "$unformatted"
  exit 1
fi
go mod verify
go vet ./...
for script in scripts/*.sh; do
  bash -n "$script"
done
CGO_ENABLED=1 go test -race -coverprofile=coverage.out ./...
CGO_ENABLED=0 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
while IFS= read -r -d '' file; do
  node --check "$file"
done < <(find static -name '*.js' -print0)
node --test tests/*.test.mjs
