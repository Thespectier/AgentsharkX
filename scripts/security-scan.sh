#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

VITE_ENABLE_MOCKS=false npm --prefix apps/web run build >/dev/null

if command -v go >/dev/null 2>&1; then
  (cd apps/server && go vet ./...)
else
  docker run --rm -v "$root_dir:/src" -w /src/apps/server golang:1.26.5-alpine go vet ./...
fi

audit_json="$(mktemp)"
trap 'rm -f "$audit_json"' EXIT
audit_status=0
npm --prefix apps/web audit --omit=dev --json >"$audit_json" || audit_status=$?

summary="$(node -e '
const fs = require("node:fs");
const report = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const counts = report.metadata?.vulnerabilities ?? {};
const high = Number(counts.high ?? 0) + Number(counts.critical ?? 0);
if (high > 0) process.exit(2);
process.stdout.write([counts.info ?? 0, counts.low ?? 0, counts.moderate ?? 0, counts.high ?? 0, counts.critical ?? 0].join("|"));
' "$audit_json")" || {
  echo "security scan: npm production dependencies contain high or critical advisories" >&2
  exit 1
}
if [[ "$audit_status" -ne 0 ]]; then
  echo "security scan: npm audit did not complete cleanly" >&2
  exit 1
fi

IFS='|' read -r info low moderate high critical <<<"$summary"
./scripts/secret-scan.sh
echo "security scan: ok (npm info=$info low=$low moderate=$moderate high=$high critical=$critical)"
