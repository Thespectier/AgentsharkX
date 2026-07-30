#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
action="${1:-}"

env_value() {
  local key="$1"
  awk -v key="$key" 'index($0, key "=") == 1 { value = substr($0, length(key) + 2) } END { print value }' "$root_dir/.env"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required for the Demo Lab stack" >&2
    exit 1
  fi
}

require_local_bind() {
  local bind demo_gateway_bind
  bind="$(env_value AGENTSHARK_BIND)"
  case "$bind" in
    127.0.0.1 | localhost | ::1)
      ;;
    *)
      echo "Demo Lab requires AGENTSHARK_BIND to be loopback-only; found ${bind:-unset}" >&2
      exit 1
      ;;
  esac
  demo_gateway_bind="$(env_value AGENTSHARK_DEMO_GATEWAY_ADMIN_BIND)"
  case "$demo_gateway_bind" in
    127.0.0.1 | localhost | ::1)
      ;;
    *)
      echo "Demo Lab requires AGENTSHARK_DEMO_GATEWAY_ADMIN_BIND to be loopback-only; found ${demo_gateway_bind:-unset}" >&2
      exit 1
      ;;
  esac
}

prepare_gateway_runtime() {
  local config_path="$root_dir/deploy/agentgateway/config.yaml"
  local gateway_data_path="$root_dir/.cache/agentgateway-standalone/data"

  if stat -c '%u' "$config_path" >/dev/null 2>&1; then
    export AGENTGATEWAY_RUNTIME_UID="$(stat -c '%u' "$config_path")"
    export AGENTGATEWAY_RUNTIME_GID="$(stat -c '%g' "$config_path")"
  else
    export AGENTGATEWAY_RUNTIME_UID="$(stat -f '%u' "$config_path")"
    export AGENTGATEWAY_RUNTIME_GID="$(stat -f '%g' "$config_path")"
  fi
  mkdir -p "$gateway_data_path"
  chmod 0700 "$gateway_data_path"
}

demo_compose() {
  docker compose \
    --env-file "$root_dir/deploy/versions.env" \
    --env-file "$root_dir/.env" \
    -f "$root_dir/deploy/compose.yaml" \
    -f "$root_dir/deploy/compose.demo.yaml" \
    "$@"
}

case "$action" in
  up)
    require_command docker
    "$root_dir/scripts/bootstrap-preview.sh"
    require_local_bind
    prepare_gateway_runtime
    "$root_dir/scripts/agentgateway-standalone.sh" stop >/dev/null 2>&1 || true
    # Each pair shares one image tag. Build one representative service first
    # so BuildKit never races while exporting the same tag concurrently.
    demo_compose build agentshark agentguard demo-runner
    demo_compose up --detach --force-recreate --wait --wait-timeout 240
    echo "Demo Lab is available at http://127.0.0.1:$(env_value AGENTSHARK_PORT)/demo"
    ;;
  status)
    if [[ ! -f "$root_dir/.env" ]]; then
      echo "Demo Lab has not been bootstrapped; run make demo-up first." >&2
      exit 1
    fi
    demo_compose ps \
      agentshark agentshark-collector postgres agentgateway agentguard \
      demo-fixtures agentshark-demo-gateway demo-runner
    ;;
  down)
    if [[ -f "$root_dir/.env" ]]; then
      demo_compose stop demo-runner agentshark-demo-gateway demo-fixtures
      demo_compose rm --force demo-runner agentshark-demo-gateway demo-fixtures
    fi
    "$root_dir/scripts/preview.sh" up
    ;;
  *)
    echo "usage: $0 {up|status|down}" >&2
    exit 2
    ;;
esac
