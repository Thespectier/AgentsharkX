#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$root_dir/.cache/release-e2e"
mkdir -p "$work_dir"

fixture_pid=""
server_pid=""
preview_pid=""
database_container=""
session_cookie="$work_dir/restart-session.cookies"
audit_after_restart="$work_dir/audit-after-restart.json"
stream_before_restart="$work_dir/stream-before-restart.txt"
stream_after_restart="$work_dir/stream-after-restart.txt"
cleanup() {
  for pid in "$preview_pid" "$server_pid" "$fixture_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  if [[ -n "$database_container" ]]; then
    docker rm -fv "$database_container" >/dev/null 2>&1 || true
  fi
  rm -f "$session_cookie" "$audit_after_restart" "$stream_before_restart" "$stream_after_restart"
}
trap cleanup EXIT

build_go() {
  if command -v go >/dev/null 2>&1; then
    (cd "$root_dir/apps/server" && CGO_ENABLED=0 go build -o "$work_dir/e2e-upstreams" ./cmd/e2e-upstreams && CGO_ENABLED=0 go build -o "$work_dir/agentshark" ./cmd/agentshark)
  else
    docker run --rm -e CGO_ENABLED=0 -v "$root_dir:/src" -w /src/apps/server golang:1.26.5-alpine \
      sh -c 'go build -o /src/.cache/release-e2e/e2e-upstreams ./cmd/e2e-upstreams && go build -o /src/.cache/release-e2e/agentshark ./cmd/agentshark'
  fi
}

wait_for() {
  local url="$1"
  for _ in $(seq 1 80); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  echo "release E2E service did not become ready: $url" >&2
  return 1
}

start_database() {
  local postgres_image postgres_version database_password database_port
  postgres_image="$(sed -n 's/^POSTGRES_IMAGE=//p' "$root_dir/deploy/versions.env")"
  postgres_version="$(sed -n 's/^POSTGRES_VERSION=//p' "$root_dir/deploy/versions.env")"
  if [[ -z "$postgres_image" || ! "$postgres_version" =~ @sha256:[[:xdigit:]]{64}$ ]]; then
    echo "release E2E requires the pinned PostgreSQL image from deploy/versions.env" >&2
    return 1
  fi

  database_password="$(openssl rand -hex 24)"
  database_container="agentsharkx-release-e2e-postgres-$$"
  docker run -d --name "$database_container" \
    -e POSTGRES_DB=agentshark_release_e2e \
    -e POSTGRES_USER=agentshark \
    -e "POSTGRES_PASSWORD=$database_password" \
    -p 127.0.0.1::5432 \
  "$postgres_image:$postgres_version" >/dev/null

  for _ in $(seq 1 120); do
    if docker exec "$database_container" psql -qAt -U agentshark -d agentshark_release_e2e -c 'SELECT 1' 2>/dev/null | grep -qx '1'; then
      database_port="$(docker port "$database_container" 5432/tcp)"
      database_port="${database_port##*:}"
      AGENTSHARK_RELEASE_DATABASE_URL="postgresql://agentshark:$database_password@127.0.0.1:$database_port/agentshark_release_e2e?sslmode=disable"
      return 0
    fi
    sleep 0.25
  done
  docker logs "$database_container" >&2 || true
  echo "release E2E PostgreSQL did not become ready" >&2
  return 1
}

start_server() {
  AGENTSHARK_LISTEN_ADDR=127.0.0.1:19080 \
  AGENTSHARK_ENVIRONMENT=local \
  AGENTSHARK_ADMIN_TOKEN=release-admin-token-with-entropy \
  AGENTSHARK_COOKIE_SECURE=false \
  AGENTGATEWAY_BASE_URL=http://127.0.0.1:19000 \
  AGENTGATEWAY_CONSOLE_URL=http://127.0.0.1:19000/ui \
  AGENTGUARD_BASE_URL=http://127.0.0.1:19001 \
  AGENTGUARD_ADMIN_TOKEN=release-guard-token-with-entropy \
  AGENTGUARD_CONSOLE_URL=http://127.0.0.1:19001 \
  AGENTGUARD_VERSION=main-4b755fb \
  AGENTSHARK_POLL_INTERVAL=1s \
  AGENTSHARK_DATABASE_URL="$AGENTSHARK_RELEASE_DATABASE_URL" \
  "$work_dir/agentshark" >>"$work_dir/server.log" 2>&1 &
  server_pid=$!
}

login_for_restart_check() {
  rm -f "$session_cookie"
  curl -fsS -o /dev/null -c "$session_cookie" \
    -H 'Content-Type: application/json' \
    --data '{"token":"release-admin-token-with-entropy"}' \
    http://127.0.0.1:19080/api/v1/auth/session
}

build_go
start_database
AGENTSHARK_E2E_GUARD_ADDR=0.0.0.0:19001 \
  "$work_dir/e2e-upstreams" >"$work_dir/upstreams.log" 2>&1 &
fixture_pid=$!
wait_for "http://127.0.0.1:19000/api/runtime"

: >"$work_dir/server.log"
start_server
wait_for "http://127.0.0.1:19080/readyz"

VITE_ENABLE_MOCKS=false npm --prefix "$root_dir/apps/web" run build >/dev/null
VITE_ENABLE_MOCKS=false VITE_BFF_PROXY_TARGET=http://127.0.0.1:19080 \
  npm --prefix "$root_dir/apps/web" run dev -- --host 0.0.0.0 >"$work_dir/preview.log" 2>&1 &
preview_pid=$!
wait_for "http://127.0.0.1:5173/"

chrome_paths="$(compgen -G "$root_dir/apps/web/.cache/ms-playwright/chromium-*/chrome-linux*/chrome" || true)"
chrome_path="${chrome_paths%%$'\n'*}"
if [[ -n "$chrome_path" ]] && "$chrome_path" --version >/dev/null 2>&1; then
  (
    cd "$root_dir/apps/web"
    AGENTSHARK_RELEASE_E2E=1 PLAYWRIGHT_BROWSERS_PATH="$root_dir/apps/web/.cache/ms-playwright" \
      npm exec playwright -- test --config playwright.release.config.ts
  )
else
  docker run --rm --add-host host.docker.internal:host-gateway \
    --user "$(id -u):$(id -g)" \
    -e HOME=/tmp \
    -v "$root_dir:/work" -w /work/apps/web \
    -e AGENTSHARK_RELEASE_E2E=1 \
    -e AGENTSHARK_RELEASE_BASE_URL=http://host.docker.internal:5173 \
    -e AGENTSHARK_RELEASE_FIXTURE_URL=http://host.docker.internal:19001 \
    -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    mcr.microsoft.com/playwright:v1.61.1-noble \
    npm exec playwright -- test --config playwright.release.config.ts
fi

login_for_restart_check
stream_status=0
curl -sS -N --max-time 2 -b "$session_cookie" \
  http://127.0.0.1:19080/api/v1/stream \
  >"$stream_before_restart" 2>/dev/null || stream_status=$?
if [[ "$stream_status" -ne 0 && "$stream_status" -ne 28 ]]; then
  echo "release E2E could not capture the pre-restart SSE cursor" >&2
  exit 1
fi
last_event_id="$(awk '/^id: [0-9]+$/ { value = $2 } END { print value }' "$stream_before_restart")"
if [[ ! "$last_event_id" =~ ^[0-9]+$ ]]; then
  echo "release E2E did not receive a persistent SSE ID before restart" >&2
  exit 1
fi

kill "$server_pid"
wait "$server_pid"
server_pid=""
start_server
wait_for "http://127.0.0.1:19080/readyz"
login_for_restart_check
curl -fsS -b "$session_cookie" \
  'http://127.0.0.1:19080/api/v1/audit/events?limit=100' \
  >"$audit_after_restart"
jq -e '
  .data.items | map(.id) as $ids |
  ($ids | index("gateway:fixture-request-1")) != null and
  ($ids | index("guard:fixture-event-1")) != null
' "$audit_after_restart" >/dev/null

stream_status=0
curl -sS -N --max-time 2 -b "$session_cookie" \
  -H "Last-Event-ID: $last_event_id" \
  http://127.0.0.1:19080/api/v1/stream \
  >"$stream_after_restart" 2>/dev/null || stream_status=$?
if [[ "$stream_status" -ne 0 && "$stream_status" -ne 28 ]] ||
  grep -q '^event: reset$' "$stream_after_restart"; then
  echo "release E2E SSE resume reset or failed after BFF restart" >&2
  exit 1
fi
resumed_event_id="$(awk '/^id: [0-9]+$/ { print $2; exit }' "$stream_after_restart")"
if [[ ! "$resumed_event_id" =~ ^[0-9]+$ ]] || ((resumed_event_id <= last_event_id)); then
  echo "release E2E did not resume from the persisted SSE cursor after restart" >&2
  exit 1
fi

echo "release E2E: ok"
