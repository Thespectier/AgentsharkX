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
  scripts/release-e2e.sh
  scripts/secret-scan.sh
)

for file in "${required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing or empty required file: $file" >&2
    exit 1
  fi
done

if rg -n --glob '!Agentshark_New_Repository_Codex_Execution_Plan.md' \
  --glob '!scripts/verify-repository.sh' \
  '(^|[/:@-])latest([^[:alnum:]_]|$)' deploy; then
  echo "unpinned latest reference found under deploy/" >&2
  exit 1
fi

if git submodule status 2>/dev/null | rg -q '.'; then
  echo "git submodules are not allowed" >&2
  exit 1
fi

echo "repository invariants: ok"
