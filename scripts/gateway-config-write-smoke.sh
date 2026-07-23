#!/usr/bin/env bash
set -euo pipefail

gateway_admin="${AGENTGATEWAY_SMOKE_BASE_URL:-http://127.0.0.1:15000}"
umask 077
payload="$(mktemp)"
response="$(mktemp)"
trap 'rm -f "$payload" "$response"' EXIT

# The payload may contain provider credentials. Keep it in a mode-0600
# temporary file, never print it, and submit it unchanged just as the native
# Raw Configuration editor does.
curl -fsS "$gateway_admin/api/config" -o "$payload"
status="$(
  curl -sS \
    -o "$response" \
    -w '%{http_code}' \
    -X POST \
    -H 'Content-Type: application/json' \
    --data-binary "@$payload" \
    "$gateway_admin/api/config"
)"

if [[ "$status" != "200" ]]; then
  echo "agentgateway Raw Config write failed with HTTP $status" >&2
  exit 1
fi
jq -e '.status == "success"' "$response" >/dev/null

echo "agentgateway Raw Config write: ok"
