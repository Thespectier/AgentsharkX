#!/usr/bin/env bash
set -euo pipefail

bundle=apps/web/dist
if [[ ! -d "$bundle" ]]; then
  echo "frontend bundle missing; run npm --prefix apps/web run build first" >&2
  exit 1
fi

for forbidden in \
  AGENTGUARD_ADMIN_TOKEN \
  AGENTGATEWAY_ADMIN_TOKEN \
  AGENTSHARK_ADMIN_TOKEN \
  change-me-before-use; do
  if rg -Fq "$forbidden" "$bundle"; then
    echo "secret boundary violation in frontend bundle: $forbidden" >&2
    exit 1
  fi
done

echo "frontend secret boundary: ok"
