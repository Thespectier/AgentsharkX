#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config_path="$root_dir/deploy/agentgateway/config.yaml"

if stat -c '%u' "$config_path" >/dev/null 2>&1; then
  config_uid="$(stat -c '%u' "$config_path")"
  config_gid="$(stat -c '%g' "$config_path")"
else
  config_uid="$(stat -f '%u' "$config_path")"
  config_gid="$(stat -f '%g' "$config_path")"
fi

# The pinned image defaults to UID 65532. A bind-mounted 0644 file owned by the
# checkout user is still unwritable even when Docker reports the mount as rw.
# Run only agentgateway as the file owner; the service remains non-root.
export AGENTGATEWAY_RUNTIME_UID="${AGENTGATEWAY_RUNTIME_UID:-$config_uid}"
export AGENTGATEWAY_RUNTIME_GID="${AGENTGATEWAY_RUNTIME_GID:-$config_gid}"

exec docker compose \
  --env-file "$root_dir/deploy/versions.env" \
  --env-file "$root_dir/.env" \
  -f "$root_dir/deploy/compose.yaml" \
  "$@"
