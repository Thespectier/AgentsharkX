#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$root_dir/.env"
template="$root_dir/deploy/example.env"

if [[ -e "$target" ]]; then
  echo ".env already exists; leaving it unchanged" >&2
  exit 1
fi
if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate preview credentials" >&2
  exit 1
fi

umask 077
cp "$template" "$target"
admin_token="$(openssl rand -hex 24)"
guard_token="$(openssl rand -hex 24)"
sed -i "s/^AGENTSHARK_ADMIN_TOKEN=.*/AGENTSHARK_ADMIN_TOKEN=$admin_token/" "$target"
sed -i "s/^AGENTGUARD_ADMIN_TOKEN=.*/AGENTGUARD_ADMIN_TOKEN=$guard_token/" "$target"

echo "Created .env with mode 0600 and generated non-placeholder credentials."
echo "Review bind addresses before exposing the preview beyond loopback."
