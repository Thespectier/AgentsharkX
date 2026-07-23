#!/usr/bin/env bash
set -euo pipefail
umask 077

admin_url="${AGENTGATEWAY_ADMIN_URL:-http://127.0.0.1:15000}"
expected_database_url="sqlite://.cache/agentgateway-standalone/data/request-logs.db"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

config_file="$work_dir/config.json"
logs_file="$work_dir/logs.json"
analytics_file="$work_dir/analytics.json"

curl -fsS "$admin_url/api/config" -o "$config_file"
jq -e --arg expected "$expected_database_url" \
  '.config.database.url == $expected' "$config_file" >/dev/null

curl -fsS \
  -H 'Content-Type: application/json' \
  --data '{"limit":10,"includeAttributes":false}' \
  "$admin_url/api/logs/search" \
  -o "$logs_file"
jq -e '
  (.logs | type == "array") and
  ([.logs[] | has("attributes") or has("payload")] | any | not)
' "$logs_file" >/dev/null

curl -fsS \
  -H 'Content-Type: application/json' \
  --data '{"bucketCount":12}' \
  "$admin_url/api/logs/analytics/summary" \
  -o "$analytics_file"
jq -e '
  (.bucketSeconds | type == "number" and . > 0) and
  (.buckets | type == "array")
' "$analytics_file" >/dev/null

log_count="$(jq '.logs | length' "$logs_file")"
request_count="$(jq '[.buckets[].requests] | add // 0' "$analytics_file")"
echo "agentgateway observability: ok (logs=$log_count analytics_requests=$request_count)"
