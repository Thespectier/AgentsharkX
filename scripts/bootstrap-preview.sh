#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$root_dir/.env"
template="$root_dir/deploy/example.env"
gateway_target="$root_dir/.agentgateway.env"
gateway_template="$root_dir/deploy/agentgateway/example.env"

database_setting_keys=(
  AGENTSHARK_DATABASE_AUTO_MIGRATE
  AGENTSHARK_DATABASE_MAX_CONNS
  AGENTSHARK_DATABASE_MIN_CONNS
  AGENTSHARK_DATABASE_CONNECT_TIMEOUT
  AGENTSHARK_EVENT_RETENTION
  AGENTSHARK_PAYLOAD_RETENTION
  AGENTSHARK_OUTBOX_RETENTION
  AGENTSHARK_DATABASE_BIND
  AGENTSHARK_DATABASE_PORT
)

phase14_setting_keys=(
  AGENTSHARK_TRACE_ENDPOINT
  AGENTSHARK_TRACE_CONTENT_MODE
  AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES
  AGENTSHARK_TRACE_PAYLOAD_RETENTION
  AGENTSHARK_TRACE_RETENTION
  AGENTSHARK_TRACE_EXPORT_TIMEOUT_SECONDS
  AGENTSHARK_TRACE_FLUSH_TIMEOUT_SECONDS
  AGENTSHARK_TRACE_BATCH_DELAY_MS
  AGENTSHARK_TRACE_MAX_QUEUE_SIZE
  AGENTSHARK_TRACE_MAX_EXPORT_BATCH_SIZE
  AGENTSHARK_GUARD_ENABLED
  AGENTSHARK_GUARD_FAILURE_MODE
  AGENTSHARK_GUARD_TIMEOUT_SECONDS
  AGENTSHARK_GUARD_REMOTE_RETRIES
  AGENTSHARK_COLLECTOR_LISTEN_ADDR
  AGENTSHARK_COLLECTOR_BIND
  AGENTSHARK_COLLECTOR_PORT
  AGENTSHARK_COLLECTOR_DATABASE_URL
  AGENTSHARK_COLLECTOR_DATABASE_MAX_CONNS
  AGENTSHARK_COLLECTOR_DATABASE_MIN_CONNS
  AGENTSHARK_COLLECTOR_DATABASE_CONNECT_TIMEOUT
  AGENTSHARK_COLLECTOR_REQUEST_TIMEOUT
  AGENTSHARK_COLLECTOR_MAX_COMPRESSED_BYTES
  AGENTSHARK_COLLECTOR_MAX_DECOMPRESSED_BYTES
  AGENTSHARK_COLLECTOR_MAX_SPANS_PER_REQUEST
)

require_openssl() {
  if ! command -v openssl >/dev/null 2>&1; then
    echo "openssl is required to generate preview credentials" >&2
    exit 1
  fi
}

env_key_count() {
  local file="$1"
  local key="$2"
  awk -v key="$key" 'index($0, key "=") == 1 { count++ } END { print count + 0 }' "$file"
}

env_value() {
  local file="$1"
  local key="$2"
  awk -v key="$key" 'index($0, key "=") == 1 { value = substr($0, length(key) + 2) } END { print value }' "$file"
}

template_value() {
  local key="$1"
  local count
  count="$(env_key_count "$template" "$key")"
  if [[ "$count" != "1" ]]; then
    echo "deploy/example.env must define $key exactly once" >&2
    exit 1
  fi
  env_value "$template" "$key"
}

database_url_for_password() {
  local password="$1"
  local url
  url="$(template_value AGENTSHARK_DATABASE_URL)"
  if [[ "$url" != *change-me-before-use* ]]; then
    echo "deploy/example.env database URL is missing its bootstrap placeholder" >&2
    exit 1
  fi
  printf '%s\n' "${url//change-me-before-use/$password}"
}

upgrade_existing_env() {
  local password_count url_count database_password database_url
  local database_url_pattern url_password key count value trace_token_count trace_token
  local -a additions=()

  password_count="$(env_key_count "$target" AGENTSHARK_DATABASE_PASSWORD)"
  url_count="$(env_key_count "$target" AGENTSHARK_DATABASE_URL)"
  if ((password_count > 1 || url_count > 1)); then
    echo ".env has duplicate database password or URL assignments; resolve them before retrying" >&2
    exit 1
  fi
  if ((password_count != url_count)); then
    echo ".env must define AGENTSHARK_DATABASE_PASSWORD and AGENTSHARK_DATABASE_URL together" >&2
    exit 1
  fi

  if ((password_count == 0)); then
    require_openssl
    database_password="$(openssl rand -hex 24)"
    database_url="$(database_url_for_password "$database_password")"
    additions+=(
      "AGENTSHARK_DATABASE_URL=$database_url"
      "AGENTSHARK_DATABASE_PASSWORD=$database_password"
    )
  else
    database_password="$(env_value "$target" AGENTSHARK_DATABASE_PASSWORD)"
    database_url="$(env_value "$target" AGENTSHARK_DATABASE_URL)"
    if [[ -z "$database_password" || "$database_password" == "change-me-before-use" ]]; then
      echo ".env contains an empty or placeholder database password; set a real password and matching URL" >&2
      exit 1
    fi
    database_url_pattern='^postgres(ql)?://[^:/?#]+:([^@/?#]+)@[^/?#]+'
    if [[ ! "$database_url" =~ $database_url_pattern ]]; then
      echo ".env database URL must contain an explicit PostgreSQL username and password" >&2
      exit 1
    fi
    url_password="${BASH_REMATCH[2]}"
    if [[ "$url_password" != "$database_password" ]]; then
      echo ".env database password does not match the password embedded in AGENTSHARK_DATABASE_URL" >&2
      exit 1
    fi
  fi

  for key in "${database_setting_keys[@]}"; do
    count="$(env_key_count "$target" "$key")"
    if ((count > 1)); then
      echo ".env has duplicate $key assignments; resolve them before retrying" >&2
      exit 1
    fi
    if ((count == 0)); then
      value="$(template_value "$key")"
      additions+=("$key=$value")
    fi
  done

  trace_token_count="$(env_key_count "$target" AGENTSHARK_TRACE_INGEST_TOKEN)"
  if ((trace_token_count > 1)); then
    echo ".env has duplicate AGENTSHARK_TRACE_INGEST_TOKEN assignments; resolve them before retrying" >&2
    exit 1
  fi
  if ((trace_token_count == 0)); then
    require_openssl
    additions+=("AGENTSHARK_TRACE_INGEST_TOKEN=$(openssl rand -hex 24)")
  else
    trace_token="$(env_value "$target" AGENTSHARK_TRACE_INGEST_TOKEN)"
    if [[ ${#trace_token} -lt 16 || "$trace_token" == change-me* || "$trace_token" == replace-me* ]]; then
      echo ".env contains an invalid or placeholder Trace ingest token; set a non-placeholder value of at least 16 characters" >&2
      exit 1
    fi
  fi

  for key in "${phase14_setting_keys[@]}"; do
    count="$(env_key_count "$target" "$key")"
    if ((count > 1)); then
      echo ".env has duplicate $key assignments; resolve them before retrying" >&2
      exit 1
    fi
    if ((count == 0)); then
      value="$(template_value "$key")"
      additions+=("$key=$value")
    fi
  done

  if ((${#additions[@]} == 0)); then
    echo ".env already contains the Phase 13 and Phase 14 settings; leaving existing values unchanged."
    return
  fi

  printf '\n# AgentsharkX durable storage and Phase 14 Trace ingest.\n' >>"$target"
  printf '%s\n' "${additions[@]}" >>"$target"
  chmod 0600 "$target"
  echo "Added missing Phase 13/14 settings to .env without changing existing values."
}

umask 077
if [[ ! -e "$gateway_target" ]]; then
  cp "$gateway_template" "$gateway_target"
  chmod 0600 "$gateway_target"
  echo "Created .agentgateway.env with mode 0600 for provider credentials."
fi

if [[ -e "$target" ]]; then
  upgrade_existing_env
  exit 0
fi

require_openssl
admin_token="$(openssl rand -hex 24)"
guard_token="$(openssl rand -hex 24)"
database_password="$(openssl rand -hex 24)"
trace_ingest_token="$(openssl rand -hex 24)"
database_url="$(database_url_for_password "$database_password")"
config_path="$root_dir/deploy/agentgateway/config.yaml"
if stat -c '%u' "$config_path" >/dev/null 2>&1; then
  gateway_uid="$(stat -c '%u' "$config_path")"
  gateway_gid="$(stat -c '%g' "$config_path")"
else
  gateway_uid="$(stat -f '%u' "$config_path")"
  gateway_gid="$(stat -f '%g' "$config_path")"
fi

awk \
  -v admin_token="$admin_token" \
  -v guard_token="$guard_token" \
  -v database_password="$database_password" \
  -v database_url="$database_url" \
  -v trace_ingest_token="$trace_ingest_token" \
  -v gateway_uid="$gateway_uid" \
  -v gateway_gid="$gateway_gid" '
    /^AGENTSHARK_ADMIN_TOKEN=/ { print "AGENTSHARK_ADMIN_TOKEN=" admin_token; next }
    /^AGENTGUARD_ADMIN_TOKEN=/ { print "AGENTGUARD_ADMIN_TOKEN=" guard_token; next }
    /^AGENTSHARK_DATABASE_URL=/ { print "AGENTSHARK_DATABASE_URL=" database_url; next }
    /^AGENTSHARK_DATABASE_PASSWORD=/ { print "AGENTSHARK_DATABASE_PASSWORD=" database_password; next }
    /^AGENTSHARK_TRACE_INGEST_TOKEN=/ { print "AGENTSHARK_TRACE_INGEST_TOKEN=" trace_ingest_token; next }
    /^AGENTGATEWAY_RUNTIME_UID=/ { print "AGENTGATEWAY_RUNTIME_UID=" gateway_uid; next }
    /^AGENTGATEWAY_RUNTIME_GID=/ { print "AGENTGATEWAY_RUNTIME_GID=" gateway_gid; next }
    { print }
  ' "$template" >"$target"
chmod 0600 "$target"

echo "Created .env with mode 0600 and generated non-placeholder credentials."
echo "The default preview runs agentgateway as a verified host-native binary."
echo "Review bind addresses before exposing the preview beyond loopback."
