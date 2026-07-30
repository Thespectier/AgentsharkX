#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
preview_env="$root_dir/.env"
timeout_seconds="${AGENTSHARK_DEMO_SMOKE_TIMEOUT_SECONDS:-180}"
poll_interval_seconds=1

if [[ ! "$timeout_seconds" =~ ^[0-9]+$ ]] || ((timeout_seconds < 30 || timeout_seconds > 600)); then
  echo "AGENTSHARK_DEMO_SMOKE_TIMEOUT_SECONDS must be an integer from 30 to 600" >&2
  exit 2
fi

for command_name in curl jq docker openssl; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "$command_name is required for the Demo Lab smoke test" >&2
    exit 1
  fi
done

if [[ ! -f "$preview_env" ]]; then
  echo "Demo Lab has not been bootstrapped; run make demo-up first" >&2
  exit 1
fi

env_value() {
  local key="$1"
  awk -v key="$key" 'index($0, key "=") == 1 { value = substr($0, length(key) + 2) } END { print value }' "$preview_env"
}

admin_token="$(env_value AGENTSHARK_ADMIN_TOKEN)"
agentshark_port="$(env_value AGENTSHARK_PORT)"
if [[ ${#admin_token} -lt 16 || "$admin_token" == change-me* || "$admin_token" == replace-me* ]]; then
  echo ".env does not contain a valid AgentsharkX admin token" >&2
  exit 1
fi
if [[ ! "$agentshark_port" =~ ^[0-9]+$ ]] || ((agentshark_port < 1 || agentshark_port > 65535)); then
  echo ".env does not contain a valid AGENTSHARK_PORT" >&2
  exit 1
fi

base_url="http://127.0.0.1:${agentshark_port}"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/agentsharkx-demo-smoke.XXXXXX")"
chmod 0700 "$work_dir"
cookie_jar="$work_dir/session.cookies"
csrf_headers="$work_dir/login.headers"
auth_headers="$work_dir/auth.headers"
response_file="$work_dir/response.json"
run_file="$work_dir/run.json"
trace_file="$work_dir/trace.json"
last_run_id=""
umask 077

cleanup() {
  rm -f "$work_dir"/*
  rmdir "$work_dir"
}
trap cleanup EXIT

demo_compose() {
  docker compose \
    --env-file "$root_dir/deploy/versions.env" \
    --env-file "$preview_env" \
    -f "$root_dir/deploy/compose.yaml" \
    -f "$root_dir/deploy/compose.demo.yaml" \
    "$@"
}

login() {
  local login_body="$work_dir/login.json"
  local status

  printf '%s' "$admin_token" | jq -Rs '{token: .}' >"$login_body"
  status="$(curl --silent --show-error \
    --connect-timeout 3 --max-time 10 \
    --dump-header "$csrf_headers" \
    --cookie-jar "$cookie_jar" \
    --header 'Content-Type: application/json' \
    --data-binary "@$login_body" \
    --output /dev/null --write-out '%{http_code}' \
    "$base_url/api/v1/auth/session")"
  if [[ "$status" != "204" ]]; then
    echo "Demo Lab smoke authentication failed with HTTP $status" >&2
    exit 1
  fi
  csrf_token="$(awk 'BEGIN { IGNORECASE=1 } /^X-CSRF-Token:/ { sub(/^[^:]*:[[:space:]]*/, ""); sub(/\r$/, ""); value=$0 } END { print value }' "$csrf_headers")"
  if [[ -z "$csrf_token" ]]; then
    echo "Demo Lab smoke authentication did not return a CSRF token" >&2
    exit 1
  fi
  printf 'X-CSRF-Token: %s\n' "$csrf_token" >"$auth_headers"
}

api_get() {
  local path="$1"
  local output="$2"
  curl --fail --silent --show-error \
    --connect-timeout 3 --max-time 10 \
    --cookie "$cookie_jar" \
    --output "$output" \
    "$base_url$path"
}

api_post() {
  local path="$1"
  local body_file="$2"
  local output="$3"
  local request_id="${4:-}"
  local -a headers=(
    --header 'Content-Type: application/json'
    --header "@$auth_headers"
  )
  if [[ -n "$request_id" ]]; then
    headers+=(--header "X-Request-ID: $request_id")
  fi
  curl --fail --silent --show-error \
    --connect-timeout 3 --max-time 15 \
    --cookie "$cookie_jar" \
    "${headers[@]}" \
    --data-binary "@$body_file" \
    --output "$output" \
    "$base_url$path"
}

wait_for_readiness() {
  local deadline=$((SECONDS + timeout_seconds))
  while ((SECONDS < deadline)); do
    if api_get /api/v1/demo/status "$response_file" 2>/dev/null &&
      jq -e '
        .data.enabled == true and
        .data.ready == true and
        (.data.components | length) == 7 and
        ([.data.components[] | select(.required and .status != "healthy")] | length) == 0
      ' "$response_file" >/dev/null; then
      return
    fi
    sleep "$poll_interval_seconds"
  done
  echo "Demo Lab did not become ready within ${timeout_seconds}s" >&2
  if [[ -s "$response_file" ]]; then
    jq -r '.data.components[]? | select(.status != "healthy") | "  \(.id): \(.status)"' "$response_file" >&2 || true
  fi
  exit 1
}

wait_for_bff() {
  local deadline=$((SECONDS + timeout_seconds))
  while ((SECONDS < deadline)); do
    if curl --fail --silent --show-error --connect-timeout 3 --max-time 10 \
      --output /dev/null "$base_url/readyz" 2>/dev/null; then
      return
    fi
    sleep "$poll_interval_seconds"
  done
  echo "AgentsharkX did not become ready within ${timeout_seconds}s" >&2
  exit 1
}

assert_fixed_scenarios() {
  api_get /api/v1/demo/scenarios "$response_file"
  jq -e '
    (.data | map(.id) | sort) == ["approval", "failure", "happy"] and
    (.data | length) == 3 and
    ([.data[] | select(.expectedMetrics.llmCalls != 3 or .expectedMetrics.mcpCalls != 2 or .expectedMetrics.a2aCalls != 1)] | length) == 0
  ' "$response_file" >/dev/null
}

start_run() {
  local scenario="$1"
  local request_body="$work_dir/start-${scenario}.json"
  local request_id
  request_id="demo-smoke-${scenario}-$(openssl rand -hex 12)"
  jq -n --arg scenario "$scenario" '{scenario: $scenario, delayMs: 0}' >"$request_body"
  api_post /api/v1/demo/runs "$request_body" "$response_file" "$request_id"
  jq -er --arg scenario "$scenario" '
    select(.data.scenario == $scenario) |
    select(.data.delayMs == 0) |
    .data.runId
  ' "$response_file"
}

fetch_run() {
  local run_id="$1"
  api_get "/api/v1/demo/runs/$run_id" "$run_file"
}

wait_for_approval() {
  local run_id="$1"
  local deadline=$((SECONDS + timeout_seconds))
  while ((SECONDS < deadline)); do
    if fetch_run "$run_id" 2>/dev/null; then
      if jq -e '
        .data.status == "waiting_approval" and
        .data.approval.status == "pending" and
        .data.approval.ticketId != "" and
        .data.approval.sessionId == .data.sessionId and
        .data.correlations.approval.status == "verified" and
        .data.correlations.approval.basis == "session_id" and
        .data.correlations.approval.value == .data.sessionId
      ' "$run_file" >/dev/null; then
        local ticket_id session_id
        ticket_id="$(jq -r '.data.approval.ticketId' "$run_file")"
        session_id="$(jq -r '.data.sessionId' "$run_file")"
        api_get '/api/v1/protect/approvals?limit=100' "$response_file"
        jq -e --arg ticket_id "$ticket_id" --arg session_id "$session_id" '
          [.data.items[] | select(.sessionId == $session_id)] as $matches |
          ($matches | length) == 1 and $matches[0].id == $ticket_id
        ' "$response_file" >/dev/null
        printf '%s\n' "$ticket_id"
        return
      fi
      if jq -e '.data.status | IN("succeeded", "failed", "cancelled", "interrupted", "expired")' "$run_file" >/dev/null; then
        echo "Approval run reached a terminal state before a ticket was linked" >&2
        exit 1
      fi
    fi
    sleep "$poll_interval_seconds"
  done
  echo "Approval ticket was not linked within ${timeout_seconds}s" >&2
  exit 1
}

resolve_approval() {
  local ticket_id="$1"
  local decision="$2"
  local encoded_ticket request_body
  encoded_ticket="$(jq -nr --arg value "$ticket_id" '$value | @uri')"
  request_body="$work_dir/${decision}.json"
  jq -n --arg note "Demo smoke ${decision} decision" '{note: $note, confirmed: true}' >"$request_body"
  api_post "/api/v1/protect/approvals/${encoded_ticket}/${decision}" "$request_body" "$response_file"
  jq -e --arg operation "${decision}-approval" --arg ticket "$ticket_id" '
    .data.operation == $operation and
    .data.status == "succeeded" and
    .data.source == "agentguard" and
    .data.target == $ticket
  ' "$response_file" >/dev/null
}

wait_for_completed_evidence() {
  local run_id="$1"
  local deadline=$((SECONDS + timeout_seconds))
  while ((SECONDS < deadline)); do
    if fetch_run "$run_id" 2>/dev/null; then
      if jq -e '
        .data.status == "succeeded" and
        .data.observedMetrics != null and
        .data.correlations.trace.status == "verified"
      ' "$run_file" >/dev/null; then
        return
      fi
      if jq -e '.data.status | IN("failed", "cancelled", "interrupted", "expired")' "$run_file" >/dev/null; then
        jq -r '"Demo run ended unexpectedly: status=\(.data.status) outcome=\(.data.outcome) error=\(.data.errorCode // "none")"' "$run_file" >&2
        exit 1
      fi
    fi
    sleep "$poll_interval_seconds"
  done
  echo "Demo run evidence did not converge within ${timeout_seconds}s" >&2
  exit 1
}

assert_run() {
  local scenario="$1"
  local outcome="$2"
  local llm_calls="$3"
  local mcp_calls="$4"
  local local_tool_calls="$5"
  local a2a_calls="$6"
  local human_checks="$7"
  local error_count="$8"
  local approval_status="${9:-}"

  jq -e \
    --arg scenario "$scenario" \
    --arg outcome "$outcome" \
    --arg approval_status "$approval_status" \
    --argjson llm "$llm_calls" \
    --argjson mcp "$mcp_calls" \
    --argjson local_tools "$local_tool_calls" \
    --argjson a2a "$a2a_calls" \
    --argjson humans "$human_checks" \
    --argjson errors "$error_count" '
      .data.scenario == $scenario and
      .data.status == "succeeded" and
      .data.outcome == $outcome and
      .data.fixtureVersion == "v1" and
      .data.rootAgentId == "demo-incident-investigator" and
      (.data.traceId | test("^[0-9a-f]{32}$")) and
      (.data.rootSpanId | test("^[0-9a-f]{16}$")) and
      .data.completedSteps == .data.totalSteps and
      .data.expectedMetrics == {
        llmCalls: $llm, mcpCalls: $mcp, localToolCalls: $local_tools,
        a2aCalls: $a2a, humanChecks: $humans, errorCount: $errors
      } and
      .data.observedMetrics == .data.expectedMetrics and
      .data.correlations.runId == .data.runId and
      .data.correlations.taskId == .data.taskId and
      .data.correlations.sessionId == .data.sessionId and
      .data.correlations.trace.status == "verified" and
      .data.correlations.trace.basis == "trace_id+task_id+session_id" and
      .data.correlations.trace.value == .data.traceId and
      .data.links.trace == ("/audit/traces/" + .data.traceId) and
      .data.correlations.gatewayLogs.status == "verified" and
      .data.correlations.gatewayLogs.basis == "complete_identical_agentgateway_trace_id_set" and
      .data.correlations.gatewayLogs.value == .data.traceId and
      (.data.links.gatewayLogs | test("^https?://[^ ]+/ui/llm/logs\\?log=[^ ]+$")) and
      (if $approval_status == "" then
        .data.approval == null and
        .data.correlations.approval.status == "unavailable" and
        .data.correlations.approval.basis == "session_id" and
        .data.correlations.approval.value == .data.sessionId
      else
        .data.approval.status == $approval_status and
        .data.approval.sessionId == .data.sessionId and
        .data.correlations.approval.status == "verified" and
        .data.correlations.approval.basis == "session_id" and
        .data.correlations.approval.value == .data.sessionId
      end)
    ' "$run_file" >/dev/null
}

assert_trace() {
  local trace_id task_id session_id llm_calls mcp_calls local_tool_calls a2a_calls error_count
  trace_id="$(jq -r '.data.traceId' "$run_file")"
  task_id="$(jq -r '.data.taskId' "$run_file")"
  session_id="$(jq -r '.data.sessionId' "$run_file")"
  llm_calls="$(jq -r '.data.expectedMetrics.llmCalls' "$run_file")"
  mcp_calls="$(jq -r '.data.expectedMetrics.mcpCalls' "$run_file")"
  local_tool_calls="$(jq -r '.data.expectedMetrics.localToolCalls' "$run_file")"
  a2a_calls="$(jq -r '.data.expectedMetrics.a2aCalls' "$run_file")"
  error_count="$(jq -r '.data.expectedMetrics.errorCount' "$run_file")"
  api_get "/api/v1/audit/traces/$trace_id" "$trace_file"
  jq -e \
    --arg trace "$trace_id" --arg task "$task_id" --arg session "$session_id" \
    --argjson llm "$llm_calls" --argjson mcp "$mcp_calls" \
    --argjson local_tools "$local_tool_calls" --argjson a2a "$a2a_calls" \
    --argjson errors "$error_count" '
      .data.summary.traceId == $trace and
      .data.summary.taskId == $task and
      .data.summary.sessionId == $session and
      .data.summary.rootAgentId == "demo-incident-investigator" and
      .data.summary.status == "succeeded" and
      .data.summary.completeness == "verified" and
      .data.summary.llmCalls == $llm and
      .data.summary.mcpCalls == $mcp and
      .data.summary.localToolCalls == $local_tools and
      .data.summary.a2aCalls == $a2a and
      .data.summary.errorCount == $errors and
      .data.rootSpan.traceId == $trace and
      .data.rootSpan.spanId == .data.summary.rootSpanId
    ' "$trace_file" >/dev/null
}

wait_for_gateway_logs() {
  local trace_id="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local count
  while ((SECONDS < deadline)); do
    count="$(demo_compose exec --no-TTY \
      --env "DEMO_TRACE_ID=$trace_id" \
      demo-fixtures python -c '
import datetime
import json
import os
import urllib.request

now = datetime.datetime.now(datetime.timezone.utc)
body = json.dumps({
    "limit": 500,
    "timeRange": {
        "from": (now - datetime.timedelta(hours=1)).isoformat().replace("+00:00", "Z"),
        "to": (now + datetime.timedelta(minutes=1)).isoformat().replace("+00:00", "Z"),
    },
    "includeAttributes": False,
}).encode()
request = urllib.request.Request(
    "http://agentshark-demo-gateway:15000/api/logs/search",
    data=body,
    headers={"Content-Type": "application/json"},
    method="POST",
)
with urllib.request.urlopen(request, timeout=3) as response:
    logs = json.load(response).get("logs", [])
print(sum(1 for entry in logs if entry.get("traceId") == os.environ["DEMO_TRACE_ID"]))
' 2>/dev/null || true)"
    if [[ "$count" == "3" ]]; then
      return
    fi
    sleep "$poll_interval_seconds"
  done
  echo "agentgateway did not expose exactly three logs for Trace $trace_id" >&2
  exit 1
}

run_and_verify() {
  local label="$1"
  local scenario="$2"
  local outcome="$3"
  local local_tool_calls="$4"
  local human_checks="$5"
  local error_count="$6"
  local decision="${7:-}"
  local approval_status=""
  local run_id ticket_id trace_id

  echo "Demo smoke: $label"
  run_id="$(start_run "$scenario")"
  if [[ -n "$decision" ]]; then
    ticket_id="$(wait_for_approval "$run_id")"
    resolve_approval "$ticket_id" "$decision"
    case "$decision" in
      approve) approval_status=approved ;;
      deny) approval_status=denied ;;
    esac
  fi
  wait_for_completed_evidence "$run_id"
  trace_id="$(jq -r '.data.traceId' "$run_file")"
  wait_for_gateway_logs "$trace_id"
  fetch_run "$run_id"
  assert_run "$scenario" "$outcome" 3 2 "$local_tool_calls" 1 "$human_checks" "$error_count" "$approval_status"
  assert_trace
  last_run_id="$run_id"
}

wait_for_bff
login
wait_for_readiness
assert_fixed_scenarios

run_and_verify "happy" happy normal 1 0 0
run_and_verify "approval / approve" approval approved 2 1 0 approve
run_and_verify "approval / deny" approval denied 2 1 0 deny
run_and_verify "failure / degraded" failure degraded 1 0 1

echo "Demo smoke: BFF restart persistence"
demo_compose restart agentshark >/dev/null
wait_for_bff
login
wait_for_readiness
fetch_run "$last_run_id"
assert_run failure degraded 3 2 1 1 0 1
assert_trace
api_get '/api/v1/demo/runs?limit=100' "$response_file"
jq -e --arg run_id "$last_run_id" '.data.items | any(.runId == $run_id)' "$response_file" >/dev/null

echo "Demo smoke: all fixed scenarios and exact evidence passed"
