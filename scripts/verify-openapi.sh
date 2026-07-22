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
  /api/v1/trust/agents
  /api/v1/protect/policies
  /api/v1/protect/approvals
  /api/v1/audit/analytics
  /api/v1/audit/events
  /api/v1/audit/sessions
)

rg -q '^openapi: 3\.1\.0$' "$spec"
rg -q '^  version: 0\.2\.0-phase2$' "$spec"
rg -q '^paths:$' "$spec"
for path in "${required_paths[@]}"; do
  if ! rg -Fq "  $path:" "$spec"; then
    echo "OpenAPI path missing: $path" >&2
    exit 1
  fi
done

npm --prefix apps/web run api:check >/dev/null

echo "OpenAPI contract: ok"
