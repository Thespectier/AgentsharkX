#!/usr/bin/env bash
set -euo pipefail

required_files=(
  AGENTS.md
  README.md
  LICENSE
  apps/server/go.mod
  apps/web/README.md
  apps/web/package.json
  apps/web/package-lock.json
  apps/web/src/main.tsx
  api/openapi.yaml
  deploy/compose.yaml
  deploy/Dockerfile
  deploy/versions.env
  docs/quickstart.md
  docs/agent-integration.md
  docs/troubleshooting.md
  docs/release/sbom.spdx.json
  docs/release/dependency-licenses.md
  docs/release/security-scan.md
  examples/agentguard_minimal.py
  docs/architecture.md
  docs/capability-matrix.md
  docs/upstream-compatibility.md
  docs/screenshots/home-1440.png
  docs/screenshots/audit-1280.png
  docs/screenshots/connect-1280.png
  docs/screenshots/trust-1280.png
  docs/screenshots/protect-1280.png
  docs/screenshots/system-degraded-1440.png
  docs/screenshots/lighthouse-accessibility.json
  scripts/bootstrap-preview.sh
  scripts/preview-compose.sh
  scripts/gateway-config-write-smoke.sh
  scripts/release-e2e.sh
  scripts/secret-scan.sh
)

for file in "${required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing or empty required file: $file" >&2
    exit 1
  fi
done

if command -v rg >/dev/null 2>&1; then
  latest_matches="$(rg -n '(^|[/:@-])latest([^[:alnum:]_]|$)' deploy || true)"
else
  latest_matches="$(grep -RInE '(^|[/:@-])latest([^[:alnum:]_]|$)' deploy || true)"
fi
if [[ -n "$latest_matches" ]]; then
  printf '%s\n' "$latest_matches"
  echo "unpinned latest reference found under deploy/" >&2
  exit 1
fi

for variable in \
  AGENTGUARD_SERVER_PLUGIN_CONFIG \
  THOUGHT_ALIGNER_BASE_URL \
  THOUGHT_ALIGNER_MODEL \
  THOUGHT_ALIGNER_API_KEY; do
  if command -v rg >/dev/null 2>&1; then
    variable_is_forwarded="$(rg -n "^[[:space:]]+$variable:" deploy/compose.yaml || true)"
  else
    variable_is_forwarded="$(grep -nE "^[[:space:]]+$variable:" deploy/compose.yaml || true)"
  fi
  if [[ -z "$variable_is_forwarded" ]]; then
    echo "AgentGuard runtime variable is not forwarded by Compose: $variable" >&2
    exit 1
  fi
done

if [[ -n "$(git submodule status 2>/dev/null)" ]]; then
  echo "git submodules are not allowed" >&2
  exit 1
fi

echo "repository invariants: ok"
