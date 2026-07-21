#!/usr/bin/env bash
set -euo pipefail

gateway_admin="${AGENTGATEWAY_BASE_URL:-http://127.0.0.1:15000}"
gateway_ready="${AGENTGATEWAY_READINESS_URL:-http://127.0.0.1:15021/healthz/ready}"
guard_base="${AGENTGUARD_BASE_URL:-http://127.0.0.1:38080}"
guard_key="${AGENTGUARD_ADMIN_TOKEN:-}"

if [[ -z "$guard_key" ]]; then
  echo "AGENTGUARD_ADMIN_TOKEN is required" >&2
  exit 1
fi

curl -fsS "$gateway_ready" | rg -q '^ready$'
curl -fsS "$gateway_admin/api/runtime" | jq -e \
  '.build.version | type == "string"' >/dev/null
curl -fsS "$gateway_admin/config_dump" | jq -e \
  'has("binds") and has("backends") and has("routes")' >/dev/null
curl -fsS -H "X-Api-Key: $guard_key" \
  "$guard_base/v1/backend/health" | jq -e \
  '.ok == true and .service == "agentguard-server"' >/dev/null
curl -fsS -H "X-Api-Key: $guard_key" \
  "$guard_base/v1/backend/tools" | jq -e 'type == "array"' >/dev/null

echo "pinned upstream management contracts: ok"
