#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$root_dir/.cache/release-e2e"
mkdir -p "$work_dir"

fixture_pid=""
server_pid=""
preview_pid=""
cleanup() {
  for pid in "$preview_pid" "$server_pid" "$fixture_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
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

build_go
AGENTSHARK_E2E_GUARD_ADDR=0.0.0.0:19001 \
  "$work_dir/e2e-upstreams" >"$work_dir/upstreams.log" 2>&1 &
fixture_pid=$!
wait_for "http://127.0.0.1:19000/api/runtime"

AGENTSHARK_LISTEN_ADDR=127.0.0.1:19080 \
AGENTSHARK_ENVIRONMENT=local \
AGENTSHARK_ADMIN_TOKEN=release-admin-token-with-entropy \
AGENTSHARK_COOKIE_SECURE=false \
AGENTGATEWAY_BASE_URL=http://127.0.0.1:19000 \
AGENTGATEWAY_CONSOLE_URL=http://127.0.0.1:19000/ui \
AGENTGUARD_BASE_URL=http://127.0.0.1:19001 \
AGENTGUARD_ADMIN_TOKEN=release-guard-token-with-entropy \
AGENTGUARD_CONSOLE_URL=http://127.0.0.1:19001 \
AGENTGUARD_VERSION=v2.1 \
AGENTSHARK_POLL_INTERVAL=1s \
"$work_dir/agentshark" >"$work_dir/server.log" 2>&1 &
server_pid=$!
wait_for "http://127.0.0.1:19080/healthz"

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
  docker run --rm --add-host host.docker.internal:host-gateway -v "$root_dir:/work" -w /work/apps/web \
    -e AGENTSHARK_RELEASE_E2E=1 \
    -e AGENTSHARK_RELEASE_BASE_URL=http://host.docker.internal:5173 \
    -e AGENTSHARK_RELEASE_FIXTURE_URL=http://host.docker.internal:19001 \
    -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    mcr.microsoft.com/playwright:v1.61.1-noble \
    npm exec playwright -- test --config playwright.release.config.ts
fi

echo "release E2E: ok"
