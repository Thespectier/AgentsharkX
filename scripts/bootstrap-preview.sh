#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$root_dir/.env"
template="$root_dir/deploy/example.env"
gateway_target="$root_dir/.agentgateway.env"
gateway_template="$root_dir/deploy/agentgateway/example.env"

umask 077
if [[ ! -e "$gateway_target" ]]; then
  cp "$gateway_template" "$gateway_target"
  chmod 0600 "$gateway_target"
  echo "Created .agentgateway.env with mode 0600 for provider credentials."
fi

if [[ -e "$target" ]]; then
  echo ".env already exists; leaving it unchanged" >&2
  exit 0
fi
if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate preview credentials" >&2
  exit 1
fi

cp "$template" "$target"
admin_token="$(openssl rand -hex 24)"
guard_token="$(openssl rand -hex 24)"
sed -i "s/^AGENTSHARK_ADMIN_TOKEN=.*/AGENTSHARK_ADMIN_TOKEN=$admin_token/" "$target"
sed -i "s/^AGENTGUARD_ADMIN_TOKEN=.*/AGENTGUARD_ADMIN_TOKEN=$guard_token/" "$target"
config_path="$root_dir/deploy/agentgateway/config.yaml"
if stat -c '%u' "$config_path" >/dev/null 2>&1; then
  gateway_uid="$(stat -c '%u' "$config_path")"
  gateway_gid="$(stat -c '%g' "$config_path")"
else
  gateway_uid="$(stat -f '%u' "$config_path")"
  gateway_gid="$(stat -f '%g' "$config_path")"
fi
sed -i "s/^AGENTGATEWAY_RUNTIME_UID=.*/AGENTGATEWAY_RUNTIME_UID=$gateway_uid/" "$target"
sed -i "s/^AGENTGATEWAY_RUNTIME_GID=.*/AGENTGATEWAY_RUNTIME_GID=$gateway_gid/" "$target"

echo "Created .env with mode 0600 and generated non-placeholder credentials."
echo "The default preview runs agentgateway as a verified host-native binary."
echo "Review bind addresses before exposing the preview beyond loopback."
