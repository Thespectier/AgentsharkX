#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
postgres_image="$(sed -n 's/^POSTGRES_IMAGE=//p' "$root_dir/deploy/versions.env")"
postgres_version="$(sed -n 's/^POSTGRES_VERSION=//p' "$root_dir/deploy/versions.env")"
container_name="agentsharkx-postgres-test-$$"
database_password="agentsharkx-phase13-integration-test"

cleanup() {
  docker rm -fv "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required for the PostgreSQL integration test" >&2
  exit 1
fi
if [[ -z "$postgres_image" || ! "$postgres_version" =~ @sha256:[[:xdigit:]]{64}$ ]]; then
  echo "PostgreSQL must be pinned by digest in deploy/versions.env" >&2
  exit 1
fi

docker run -d --name "$container_name" \
  -e POSTGRES_DB=agentshark_test \
  -e POSTGRES_USER=agentshark \
  -e "POSTGRES_PASSWORD=$database_password" \
  -p 127.0.0.1::5432 \
  "$postgres_image:$postgres_version" >/dev/null

for _ in $(seq 1 120); do
  if docker exec "$container_name" psql -qAt -U agentshark -d agentshark_test -c 'SELECT 1' 2>/dev/null | grep -qx '1'; then
    break
  fi
  sleep 0.25
done
if ! docker exec "$container_name" psql -qAt -U agentshark -d agentshark_test -c 'SELECT 1' 2>/dev/null | grep -qx '1'; then
  docker logs "$container_name" >&2 || true
  echo "PostgreSQL integration-test database did not become ready" >&2
  exit 1
fi

host_port="$(docker port "$container_name" 5432/tcp)"
host_port="${host_port##*:}"
if [[ ! "$host_port" =~ ^[0-9]+$ ]]; then
  echo "could not resolve the PostgreSQL integration-test port" >&2
  exit 1
fi

if command -v go >/dev/null 2>&1; then
  (
    cd "$root_dir/apps/server"
    AGENTSHARK_TEST_DATABASE_URL="postgresql://agentshark:$database_password@127.0.0.1:$host_port/agentshark_test?sslmode=disable" \
      go test -count=1 -run '^TestPostgresStoreLifecycle$' ./internal/storage/postgres
  )
else
  docker run --rm --add-host host.docker.internal:host-gateway \
    -v "$root_dir:/src" -w /src/apps/server \
    -e "AGENTSHARK_TEST_DATABASE_URL=postgresql://agentshark:$database_password@host.docker.internal:$host_port/agentshark_test?sslmode=disable" \
    golang:1.26.5 \
    go test -count=1 -run '^TestPostgresStoreLifecycle$' ./internal/storage/postgres
fi
