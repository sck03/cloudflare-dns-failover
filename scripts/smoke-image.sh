#!/usr/bin/env bash
set -euo pipefail
image=${1:-cfguard:smoke}
smoke_dir=$(mktemp -d)
cleanup() {
  docker rm -f cfguard-smoke >/dev/null 2>&1 || true
  rm -rf "$smoke_dir"
}
trap cleanup EXIT
mkdir -p "$smoke_dir/config"
printf '%s\n' 'server:' '  port: 18099' '  auth_enabled: false' 'accounts: []' 'monitors: []' > "$smoke_dir/config/config.yaml"
docker run -d --name cfguard-smoke -v "$smoke_dir/config:/app/config" "$image"
healthy=false
for _ in {1..30}; do
  if docker exec cfguard-smoke ./cfguard -healthcheck >/dev/null 2>&1; then
    healthy=true
    break
  fi
  sleep 1
done
if [[ $healthy != true ]]; then
  docker logs cfguard-smoke
  exit 1
fi
docker exec cfguard-smoke ./cfguard -version
docker exec cfguard-smoke ping -c 1 -W 2 127.0.0.1
docker stop -t 10 cfguard-smoke
test "$(docker inspect --format '{{.State.ExitCode}}' cfguard-smoke)" = 0
