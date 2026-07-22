#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

if git ls-files --error-unmatch .env >/dev/null 2>&1; then
  echo "secret scan: .env must not be tracked" >&2
  exit 1
fi

patterns=(
  '-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----'
  'AKIA[0-9A-Z]{16}'
  'gh[pousr]_[A-Za-z0-9]{30,}'
  'xox[baprs]-[A-Za-z0-9-]{20,}'
  'sk-[A-Za-z0-9]{32,}'
  '[Bb]earer[[:space:]]+[A-Za-z0-9._~+/-]{24,}'
)

for pattern in "${patterns[@]}"; do
  if git grep -IEn -e "$pattern" -- \
    ':!scripts/secret-scan.sh' \
    ':!Agentshark_New_Repository_Codex_Execution_Plan.md'; then
    echo "secret scan: credential-like material matched a tracked file" >&2
    exit 1
  fi
done

./scripts/verify-secret-boundary.sh
echo "tracked-file secret scan: ok"
