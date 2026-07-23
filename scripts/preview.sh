#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
preview_env="$root_dir/.env"

runtime_mode="${AGENTGATEWAY_RUNTIME_MODE:-}"
if [[ -z "$runtime_mode" && -f "$preview_env" ]]; then
  runtime_mode="$(sed -n 's/^AGENTGATEWAY_RUNTIME_MODE=//p' "$preview_env" | tail -n 1)"
fi
runtime_mode="${runtime_mode:-standalone}"

standalone_supported() {
  case "$(uname -s)/$(uname -m)" in
    Linux/x86_64|Linux/aarch64|Linux/arm64|Darwin/arm64)
      ;;
    *)
      echo "the integrated standalone preview has no pinned binary for this platform" >&2
      echo "set AGENTGATEWAY_RUNTIME_MODE=container for the Compose fallback" >&2
      return 1
      ;;
  esac
}

up_preview() {
  case "$runtime_mode" in
    standalone)
      standalone_supported
      "$root_dir/scripts/preview-compose.sh" stop agentgateway >/dev/null 2>&1 || true
      "$root_dir/scripts/agentgateway-standalone.sh" start
      "$root_dir/scripts/standalone-compose.sh" up --build -d \
        agentshark agentguard agentguard-console
      ;;
    container)
      "$root_dir/scripts/agentgateway-standalone.sh" stop >/dev/null 2>&1 || true
      "$root_dir/scripts/preview-compose.sh" up --build -d
      ;;
    *)
      echo "unsupported AGENTGATEWAY_RUNTIME_MODE: $runtime_mode" >&2
      exit 2
      ;;
  esac
}

down_preview() {
  "$root_dir/scripts/preview-compose.sh" down
  "$root_dir/scripts/agentgateway-standalone.sh" stop
}

status_preview() {
  case "$runtime_mode" in
    standalone)
      "$root_dir/scripts/agentgateway-standalone.sh" status || true
      "$root_dir/scripts/standalone-compose.sh" ps
      ;;
    container)
      "$root_dir/scripts/preview-compose.sh" ps
      ;;
    *)
      echo "unsupported AGENTGATEWAY_RUNTIME_MODE: $runtime_mode" >&2
      exit 2
      ;;
  esac
}

case "${1:-}" in
  up)
    up_preview
    ;;
  down)
    down_preview
    ;;
  status)
    status_preview
    ;;
  *)
    echo "usage: $0 {up|down|status}" >&2
    exit 2
    ;;
esac
