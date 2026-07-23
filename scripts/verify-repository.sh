#!/usr/bin/env bash
set -euo pipefail

required_files=(
  AGENTS.md
  README.md
  LICENSE
  apps/server/go.mod
  apps/web/README.md
  apps/web/package.json
  apps/web/package-lock.json
  apps/web/src/main.tsx
  api/openapi.yaml
  deploy/compose.yaml
  deploy/compose.standalone-gateway.yaml
  deploy/compose.standalone-gateway.host-network.yaml
  deploy/Dockerfile
  deploy/versions.env
  deploy/agentgateway/example.env
  docs/quickstart.md
  docs/agent-integration.md
  docs/troubleshooting.md
  docs/release/sbom.spdx.json
  docs/release/dependency-licenses.md
  docs/release/security-scan.md
  examples/agentguard_minimal.py
  docs/architecture.md
  docs/capability-matrix.md
  docs/upstream-compatibility.md
  docs/screenshots/home-1440.png
  docs/screenshots/audit-1280.png
  docs/screenshots/connect-1280.png
  docs/screenshots/trust-1280.png
  docs/screenshots/protect-1280.png
  docs/screenshots/system-degraded-1440.png
  docs/screenshots/lighthouse-accessibility.json
  scripts/bootstrap-preview.sh
  scripts/agentgateway-standalone.sh
  scripts/standalone-compose.sh
  scripts/preview.sh
  scripts/preview-compose.sh
  scripts/gateway-config-write-smoke.sh
  scripts/gateway-observability-smoke.sh
  scripts/release-e2e.sh
  scripts/secret-scan.sh
)

for file in "${required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing or empty required file: $file" >&2
    exit 1
  fi
done

binary_version="$(sed -n 's/^AGENTGATEWAY_BINARY_VERSION=//p' deploy/versions.env)"
if [[ ! "$binary_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "agentgateway standalone binary version is not pinned to a stable release" >&2
  exit 1
fi

for variable in \
  AGENTGATEWAY_BINARY_LINUX_AMD64_SHA256 \
  AGENTGATEWAY_BINARY_LINUX_ARM64_SHA256 \
  AGENTGATEWAY_BINARY_DARWIN_ARM64_SHA256; do
  checksum="$(sed -n "s/^${variable}=//p" deploy/versions.env)"
  if [[ ! "$checksum" =~ ^[[:xdigit:]]{64}$ ]]; then
    echo "invalid or missing agentgateway binary checksum: $variable" >&2
    exit 1
  fi
done

if ! grep -qx 'AGENTGATEWAY_RUNTIME_MODE=standalone' deploy/example.env; then
  echo "standalone agentgateway must remain the default local preview mode" >&2
  exit 1
fi

if ! grep -qx 'AGENTGATEWAY_DOCKER_HOST_MODE=auto' deploy/example.env; then
  echo "standalone Docker host connector must remain auto-detected by default" >&2
  exit 1
fi

for script in \
  scripts/agentgateway-standalone.sh \
  scripts/gateway-observability-smoke.sh \
  scripts/standalone-compose.sh \
  scripts/preview.sh; do
  if [[ ! -x "$script" ]]; then
    echo "standalone preview script is not executable: $script" >&2
    exit 1
  fi
done

bash -n \
  scripts/agentgateway-standalone.sh \
  scripts/gateway-observability-smoke.sh \
  scripts/standalone-compose.sh \
  scripts/preview.sh \
  scripts/bootstrap-preview.sh

if command -v rg >/dev/null 2>&1; then
  latest_matches="$(rg -n '(^|[/:@-])latest([^[:alnum:]_]|$)' deploy || true)"
else
  latest_matches="$(grep -RInE '(^|[/:@-])latest([^[:alnum:]_]|$)' deploy || true)"
fi
if [[ -n "$latest_matches" ]]; then
  printf '%s\n' "$latest_matches"
  echo "unpinned latest reference found under deploy/" >&2
  exit 1
fi

for variable in \
  AGENTGUARD_SERVER_PLUGIN_CONFIG \
  THOUGHT_ALIGNER_BASE_URL \
  THOUGHT_ALIGNER_MODEL \
  THOUGHT_ALIGNER_API_KEY; do
  if command -v rg >/dev/null 2>&1; then
    variable_is_forwarded="$(rg -n "^[[:space:]]+$variable:" deploy/compose.yaml || true)"
  else
    variable_is_forwarded="$(grep -nE "^[[:space:]]+$variable:" deploy/compose.yaml || true)"
  fi
  if [[ -z "$variable_is_forwarded" ]]; then
    echo "AgentGuard runtime variable is not forwarded by Compose: $variable" >&2
    exit 1
  fi
done

if [[ -n "$(git submodule status 2>/dev/null)" ]]; then
  echo "git submodules are not allowed" >&2
  exit 1
fi

echo "repository invariants: ok"
