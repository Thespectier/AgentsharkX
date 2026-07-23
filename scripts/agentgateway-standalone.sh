#!/usr/bin/env bash
set -euo pipefail
umask 077

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
versions_file="$root_dir/deploy/versions.env"
preview_env="$root_dir/.env"
gateway_env="$root_dir/.agentgateway.env"
config_path="$root_dir/deploy/agentgateway/config.yaml"
cache_root="$root_dir/.cache/agentgateway-standalone"
runtime_root="$cache_root/runtime"
pid_file="$runtime_root/agentgateway.pid"
manager_file="$runtime_root/manager"
log_file="$runtime_root/agentgateway.log"
unit_suffix="$(printf '%s' "$root_dir" | cksum | awk '{print $1}')"
systemd_unit="agentsharkx-agentgateway-$unit_suffix"

# This file contains only repository-controlled immutable version metadata.
# shellcheck disable=SC1090
. "$versions_file"

binary_version="${AGENTGATEWAY_BINARY_VERSION:?missing AGENTGATEWAY_BINARY_VERSION}"
binary_dir="$cache_root/$binary_version"
binary_path="$binary_dir/agentgateway"

platform_asset() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os/$arch" in
    Linux/x86_64)
      asset_name="agentgateway-linux-amd64"
      asset_sha256="${AGENTGATEWAY_BINARY_LINUX_AMD64_SHA256:?missing Linux amd64 checksum}"
      ;;
    Linux/aarch64|Linux/arm64)
      asset_name="agentgateway-linux-arm64"
      asset_sha256="${AGENTGATEWAY_BINARY_LINUX_ARM64_SHA256:?missing Linux arm64 checksum}"
      ;;
    Darwin/arm64)
      asset_name="agentgateway-darwin-arm64"
      asset_sha256="${AGENTGATEWAY_BINARY_DARWIN_ARM64_SHA256:?missing Darwin arm64 checksum}"
      ;;
    *)
      echo "unsupported standalone platform: $os/$arch" >&2
      echo "set AGENTGATEWAY_RUNTIME_MODE=container to use the pinned container fallback" >&2
      exit 1
      ;;
  esac
}

file_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "sha256sum or shasum is required to verify agentgateway" >&2
    return 1
  fi
}

verify_binary() {
  [[ -x "$binary_path" ]] || return 1
  [[ "$(file_sha256 "$binary_path")" == "$asset_sha256" ]] || return 1
  "$binary_path" --version | jq -e \
    --arg version "${binary_version#v}" \
    --arg revision "$AGENTGATEWAY_GIT_REVISION" \
    '.version == $version and (.git_revision | startswith($revision))' >/dev/null
}

install_binary() {
  platform_asset
  command -v jq >/dev/null 2>&1 || {
    echo "jq is required to verify agentgateway version metadata" >&2
    exit 1
  }
  if verify_binary; then
    echo "agentgateway standalone binary: $binary_path"
    return
  fi
  command -v curl >/dev/null 2>&1 || {
    echo "curl is required to download agentgateway" >&2
    exit 1
  }

  local download_path download_url actual_sha256
  mkdir -p "$binary_dir"
  download_path="$(mktemp "$binary_dir/.agentgateway.download.XXXXXX")"
  download_url="https://github.com/agentgateway/agentgateway/releases/download/$binary_version/$asset_name"
  if ! curl -fsSL "$download_url" -o "$download_path"; then
    rm -f "$download_path"
    return 1
  fi
  actual_sha256="$(file_sha256 "$download_path")"
  if [[ "$actual_sha256" != "$asset_sha256" ]]; then
    rm -f "$download_path"
    echo "agentgateway checksum mismatch for $asset_name" >&2
    return 1
  fi
  chmod 0755 "$download_path"
  mv "$download_path" "$binary_path"
  verify_binary
  echo "installed verified agentgateway $binary_version at $binary_path"
}

process_matches() {
  local pid="$1" command_line
  kill -0 "$pid" 2>/dev/null || return 1
  command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command_line" == *"$binary_path"* && "$command_line" == *"$config_path"* ]]
}

systemd_user_available() {
  [[ "$(uname -s)" == "Linux" ]] &&
    command -v systemd-run >/dev/null 2>&1 &&
    command -v systemctl >/dev/null 2>&1 &&
    systemctl --user show-environment >/dev/null 2>&1
}

manager_kind() {
  [[ -s "$manager_file" ]] || return 1
  sed -n '1p' "$manager_file"
}

running_pid() {
  [[ -s "$pid_file" ]] || return 1
  local pid
  pid="$(<"$pid_file")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  if [[ "$(manager_kind 2>/dev/null || true)" == "systemd" ]]; then
    systemctl --user is-active --quiet "$systemd_unit" 2>/dev/null || return 1
  fi
  process_matches "$pid" || return 1
  printf '%s\n' "$pid"
}

load_runtime_environment() {
  if [[ ! -f "$preview_env" ]]; then
    echo ".env is missing; run make preview-bootstrap first" >&2
    exit 1
  fi

  # Read only the topology values used by this script. The Compose-style .env
  # file is data, not a shell program.
  AGENTGATEWAY_ADMIN_BIND="$(read_dotenv AGENTGATEWAY_ADMIN_BIND)"
  AGENTGATEWAY_ADMIN_PORT="$(read_dotenv AGENTGATEWAY_ADMIN_PORT)"
  AGENTGATEWAY_METRICS_BIND="$(read_dotenv AGENTGATEWAY_METRICS_BIND)"
  AGENTGATEWAY_METRICS_PORT="$(read_dotenv AGENTGATEWAY_METRICS_PORT)"
  AGENTGATEWAY_READINESS_BIND="$(read_dotenv AGENTGATEWAY_READINESS_BIND)"
  AGENTGATEWAY_READINESS_PORT="$(read_dotenv AGENTGATEWAY_READINESS_PORT)"
}

load_provider_environment() {
  if [[ -f "$gateway_env" ]]; then
    set -a
    # shellcheck disable=SC1090
    . "$gateway_env"
    set +a
  fi
}

read_dotenv() {
  local name="$1" line
  line="$(grep -E "^${name}=" "$preview_env" | tail -n 1 || true)"
  line="${line%$'\r'}"
  printf '%s' "${line#*=}"
}

start_with_systemd() {
  local -a command
  command=(
    systemd-run
    --user
    "--unit=$systemd_unit"
    --collect
    --quiet
    "--property=Type=exec"
    "--property=Restart=no"
    "--property=WorkingDirectory=$root_dir"
    "--property=StandardOutput=append:$log_file"
    "--property=StandardError=append:$log_file"
    "--setenv=ADMIN_ADDR=${AGENTGATEWAY_ADMIN_BIND:-127.0.0.1}:${AGENTGATEWAY_ADMIN_PORT:-15000}"
    "--setenv=STATS_ADDR=${AGENTGATEWAY_METRICS_BIND:-127.0.0.1}:${AGENTGATEWAY_METRICS_PORT:-15020}"
    "--setenv=READINESS_ADDR=${AGENTGATEWAY_READINESS_BIND:-127.0.0.1}:${AGENTGATEWAY_READINESS_PORT:-15021}"
  )
  if [[ -f "$gateway_env" ]]; then
    command+=("--property=EnvironmentFile=$gateway_env")
  fi
  systemctl --user reset-failed "$systemd_unit" >/dev/null 2>&1 || true
  command+=("$binary_path" -f "$config_path")
  "${command[@]}"

  local pid=""
  for _ in $(seq 1 20); do
    pid="$(systemctl --user show "$systemd_unit" --property=MainPID --value 2>/dev/null || true)"
    if [[ "$pid" =~ ^[1-9][0-9]*$ ]] && process_matches "$pid"; then
      printf '%s\n' "$pid" >"$pid_file"
      printf '%s\n' systemd >"$manager_file"
      printf '%s\n' "$pid"
      return
    fi
    sleep 0.1
  done
  systemctl --user stop "$systemd_unit" >/dev/null 2>&1 || true
  echo "systemd did not report an agentgateway MainPID" >&2
  return 1
}

start_with_nohup() {
  load_provider_environment
  ADMIN_ADDR="${AGENTGATEWAY_ADMIN_BIND:-127.0.0.1}:${AGENTGATEWAY_ADMIN_PORT:-15000}" \
  STATS_ADDR="${AGENTGATEWAY_METRICS_BIND:-127.0.0.1}:${AGENTGATEWAY_METRICS_PORT:-15020}" \
  READINESS_ADDR="${AGENTGATEWAY_READINESS_BIND:-127.0.0.1}:${AGENTGATEWAY_READINESS_PORT:-15021}" \
    nohup "$binary_path" -f "$config_path" >>"$log_file" 2>&1 &
  local pid=$!
  printf '%s\n' "$pid" >"$pid_file"
  printf '%s\n' nohup >"$manager_file"
  printf '%s\n' "$pid"
}

start_gateway() {
  platform_asset
  install_binary
  if [[ ! -r "$config_path" || ! -w "$config_path" ]]; then
    echo "agentgateway config must be readable and writable by the current user: $config_path" >&2
    exit 1
  fi
  command -v curl >/dev/null 2>&1 || {
    echo "curl is required to check agentgateway readiness" >&2
    exit 1
  }
  mkdir -p "$runtime_root"
  if pid="$(running_pid)"; then
    echo "agentgateway standalone is already running (pid $pid)"
    return
  fi
  rm -f "$pid_file" "$manager_file"
  load_runtime_environment
  : >"$log_file"

  if systemd_user_available; then
    pid="$(start_with_systemd)"
  else
    pid="$(start_with_nohup)"
  fi

  readiness_url="http://${AGENTGATEWAY_READINESS_BIND:-127.0.0.1}:${AGENTGATEWAY_READINESS_PORT:-15021}/healthz/ready"
  for _ in $(seq 1 80); do
    if curl -fsS "$readiness_url" 2>/dev/null | grep -q '^ready$'; then
      echo "agentgateway standalone started (pid $pid)"
      echo "admin: http://${AGENTGATEWAY_ADMIN_BIND:-127.0.0.1}:${AGENTGATEWAY_ADMIN_PORT:-15000}/ui"
      echo "log: $log_file"
      return
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      break
    fi
    sleep 0.25
  done

  echo "agentgateway standalone failed to become ready; inspect $log_file" >&2
  if [[ "$(manager_kind 2>/dev/null || true)" == "systemd" ]]; then
    systemctl --user stop "$systemd_unit" >/dev/null 2>&1 || true
  elif kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -f "$pid_file" "$manager_file"
  return 1
}

stop_gateway() {
  if [[ "$(manager_kind 2>/dev/null || true)" == "systemd" ]]; then
    if systemctl --user is-active --quiet "$systemd_unit" 2>/dev/null; then
      systemctl --user stop "$systemd_unit"
      rm -f "$pid_file" "$manager_file"
      echo "agentgateway standalone stopped"
      return
    fi
    rm -f "$pid_file" "$manager_file"
    echo "agentgateway standalone is not running"
    return
  fi

  if ! pid="$(running_pid)"; then
    rm -f "$pid_file" "$manager_file"
    echo "agentgateway standalone is not running"
    return
  fi
  kill "$pid"
  for _ in $(seq 1 40); do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$pid_file" "$manager_file"
      echo "agentgateway standalone stopped"
      return
    fi
    sleep 0.25
  done
  echo "agentgateway standalone did not stop within 10 seconds (pid $pid)" >&2
  return 1
}

status_gateway() {
  platform_asset
  if pid="$(running_pid)"; then
    version="$("$binary_path" --version | jq -r '.version')"
    manager="$(manager_kind 2>/dev/null || printf '%s' unknown)"
    echo "agentgateway standalone: running (pid $pid, version $version, manager $manager)"
    echo "binary: $binary_path"
    echo "config: $config_path"
    return
  fi
  echo "agentgateway standalone: stopped"
  return 1
}

command="${1:-}"
case "$command" in
  install)
    platform_asset
    install_binary
    ;;
  start)
    start_gateway
    ;;
  stop)
    stop_gateway
    ;;
  restart)
    stop_gateway
    start_gateway
    ;;
  status)
    status_gateway
    ;;
  logs)
    [[ -f "$log_file" ]] || {
      echo "no standalone log exists yet" >&2
      exit 1
    }
    tail -n "${AGENTGATEWAY_LOG_LINES:-100}" "$log_file"
    ;;
  *)
    echo "usage: $0 {install|start|stop|restart|status|logs}" >&2
    exit 2
    ;;
esac
