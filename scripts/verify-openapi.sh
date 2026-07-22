#!/usr/bin/env bash
set -euo pipefail

spec=api/openapi.yaml
required_paths=(
  /api/v1/auth/session
  /api/v1/system/health
  /api/v1/system/capabilities
  /api/v1/overview
  /api/v1/stream
  /api/v1/connect/summary
  /api/v1/connect/analytics
  /api/v1/connect/setup
  /api/v1/connect/llm/providers
  /api/v1/connect/llm/models
  /api/v1/connect/mcp/servers
  /api/v1/connect/traffic/routes
  /api/v1/trust/agents
  /api/v1/trust/resources
  /api/v1/trust/scans
  '/api/v1/trust/agents/{agentId}/tools/{tool}/labels'
  '/api/v1/trust/agents/{agentId}/skills/detect'
  '/api/v1/trust/agents/{agentId}/mcps/detect'
  /api/v1/protect/policies
  /api/v1/protect/runtime-rules/check
  '/api/v1/protect/agents/{agentId}/runtime-rules'
  '/api/v1/protect/agents/{agentId}/runtime-rules/{ruleId}'
  /api/v1/protect/approvals
  '/api/v1/protect/approvals/{ticketId}/approve'
  '/api/v1/protect/approvals/{ticketId}/deny'
  /api/v1/audit/analytics
  /api/v1/audit/events
  '/api/v1/audit/events/{source}/{eventId}'
  /api/v1/audit/sessions
)

rg -q '^openapi: 3\.1\.0$' "$spec"
rg -q '^  version: 0\.6\.0-phase6$' "$spec"
rg -q '^paths:$' "$spec"
for path in "${required_paths[@]}"; do
  if ! rg -Fq "  $path:" "$spec"; then
    echo "OpenAPI path missing: $path" >&2
    exit 1
  fi
done

npm --prefix apps/web run api:check >/dev/null

echo "OpenAPI contract: ok"
