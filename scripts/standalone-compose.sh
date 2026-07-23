#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
preview_env="$root_dir/.env"

host_mode="${AGENTGATEWAY_DOCKER_HOST_MODE:-}"
if [[ -z "$host_mode" && -f "$preview_env" ]]; then
  host_mode="$(sed -n 's/^AGENTGATEWAY_DOCKER_HOST_MODE=//p' "$preview_env" | tail -n 1)"
fi
host_mode="${host_mode:-auto}"

if [[ "$host_mode" == "auto" ]]; then
  docker_os="$(docker info --format '{{.OperatingSystem}}')"
  if [[ "$docker_os" == *"Docker Desktop"* ]]; then
    host_mode="desktop"
  elif [[ "$(uname -s)" == "Linux" ]]; then
    host_mode="host-network"
  else
    echo "cannot auto-select a standalone Docker host connector" >&2
    echo "set AGENTGATEWAY_DOCKER_HOST_MODE=desktop or use container mode" >&2
    exit 1
  fi
fi

case "$host_mode" in
  desktop)
    overlay="$root_dir/deploy/compose.standalone-gateway.yaml"
    ;;
  host-network)
    if [[ "$(uname -s)" != "Linux" ]]; then
      echo "host-network standalone integration requires native Linux Docker" >&2
      exit 1
    fi
    overlay="$root_dir/deploy/compose.standalone-gateway.host-network.yaml"
    ;;
  *)
    echo "unsupported AGENTGATEWAY_DOCKER_HOST_MODE: $host_mode" >&2
    exit 2
    ;;
esac

exec docker compose \
  --env-file "$root_dir/deploy/versions.env" \
  --env-file "$root_dir/.env" \
  -f "$root_dir/deploy/compose.yaml" \
  -f "$overlay" \
  "$@"
