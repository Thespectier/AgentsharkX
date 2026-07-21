#!/usr/bin/env bash
set -euo pipefail

required_files=(
  AGENTS.md
  README.md
  LICENSE
  apps/server/go.mod
  apps/web/README.md
  api/openapi.yaml
  deploy/compose.yaml
  deploy/versions.env
  docs/architecture.md
  docs/capability-matrix.md
  docs/upstream-compatibility.md
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
