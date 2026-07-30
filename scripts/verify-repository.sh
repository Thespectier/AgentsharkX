#!/usr/bin/env bash
set -euo pipefail

required_files=(
  AGENTS.md
  README.md
  LICENSE
  apps/server/go.mod
  apps/server/go.sum
  apps/server/migrations/embed.go
  apps/server/migrations/000001_persistent_audit.sql
  apps/server/migrations/000002_trace_ingest.sql
  apps/server/migrations/000003_trace_query.sql
  apps/server/migrations/000004_demo_lab.sql
  apps/server/cmd/agentshark-collector/main.go
  apps/server/cmd/e2e-trace/main.go
  apps/server/internal/telemetry/normalize/normalize.go
  apps/server/internal/telemetry/receiver/handler.go
  apps/web/README.md
  apps/web/package.json
  apps/web/package-lock.json
  apps/web/src/main.tsx
  api/openapi.yaml
  deploy/compose.yaml
  deploy/compose.standalone-gateway.yaml
  deploy/compose.standalone-gateway.host-network.yaml
  deploy/compose.demo.yaml
  deploy/Dockerfile
  deploy/versions.env
  deploy/agentgateway/example.env
  deploy/agentgateway/demo-config.yaml
  docs/demo-lab.md
  docs/quickstart.md
  docs/database.md
  docs/agent-integration.md
  docs/troubleshooting.md
  docs/release/sbom.spdx.json
  docs/release/dependency-licenses.md
  docs/release/security-scan.md
  examples/agentguard_minimal.py
  examples/agentshark_trace_minimal.py
  examples/demo-agent/pyproject.toml
  examples/demo-agent/uv.lock
  examples/demo-agent/Dockerfile
  sdk/python/pyproject.toml
  sdk/python/constraints.txt
  sdk/python/README.md
  sdk/python/src/agentshark/runtime.py
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
  scripts/bootstrap-sdk.sh
  scripts/verify-agentguard-sdk.sh
  scripts/agentgateway-standalone.sh
  scripts/standalone-compose.sh
  scripts/preview.sh
  scripts/preview-compose.sh
  scripts/demo.sh
  scripts/demo-smoke.sh
  scripts/gateway-config-write-smoke.sh
  scripts/gateway-observability-smoke.sh
  scripts/release-e2e.sh
  scripts/test-postgres.sh
  scripts/secret-scan.sh
)

for file in "${required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing or empty required file: $file" >&2
    exit 1
  fi
done

agentshark_version="$(sed -n 's/^AGENTSHARK_VERSION=//p' deploy/versions.env)"
if [[ -z "$agentshark_version" ]] ||
  ! grep -Fqx "  version: $agentshark_version" api/openapi.yaml ||
  ! grep -Fqx "ARG AGENTSHARK_VERSION=$agentshark_version" deploy/Dockerfile ||
  ! grep -Fq -- "--build-arg AGENTSHARK_VERSION=$agentshark_version" Makefile; then
  echo "AgentsharkX version must match across versions.env, OpenAPI, Dockerfile, and Makefile" >&2
  exit 1
fi

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

postgres_image="$(sed -n 's/^POSTGRES_IMAGE=//p' deploy/versions.env)"
postgres_version="$(sed -n 's/^POSTGRES_VERSION=//p' deploy/versions.env)"
if [[ "$postgres_image" != "postgres" || ! "$postgres_version" =~ ^[0-9]+\.[0-9]+-alpine[0-9]+\.[0-9]+@sha256:[[:xdigit:]]{64}$ ]]; then
  echo "PostgreSQL image must use a versioned Alpine tag plus immutable digest" >&2
  exit 1
fi

if ! grep -qx 'AGENTSHARK_DATABASE_BIND=127.0.0.1' deploy/example.env; then
  echo "the default PostgreSQL host publication must remain on loopback" >&2
  exit 1
fi

if ! grep -qx 'AGENTSHARK_DATABASE_AUTO_MIGRATE=true' deploy/example.env ||
  ! grep -Fqx 'COPY --from=server-build /out/agentshark-migrate /usr/local/bin/agentshark-migrate' deploy/Dockerfile; then
  echo "preview migrations must remain enabled and the production migration binary must remain packaged" >&2
  exit 1
fi

if ! grep -qx 'AGENTSHARK_COLLECTOR_BIND=127.0.0.1' deploy/example.env ||
  ! grep -qx 'AGENTSHARK_TRACE_CONTENT_MODE=metadata' deploy/example.env; then
  echo "the default Trace Collector must remain loopback-published and metadata-only" >&2
  exit 1
fi

if ! grep -qx 'AGENTSHARK_DEMO_GATEWAY_ADMIN_BIND=127.0.0.1' deploy/example.env ||
  ! grep -Fqx '      - "${AGENTSHARK_DEMO_GATEWAY_ADMIN_BIND:-127.0.0.1}:${AGENTSHARK_DEMO_GATEWAY_ADMIN_PORT:-15010}:15000"' deploy/compose.demo.yaml; then
  echo "the Demo gateway management console must remain loopback-published" >&2
  exit 1
fi

if ! grep -Fqx '      AGENTSHARK_COLLECTOR_LISTEN_ADDR: 0.0.0.0:4318' deploy/compose.yaml ||
  ! grep -Fqx '      AGENTSHARK_TRACE_RETENTION: ${AGENTSHARK_TRACE_RETENTION}' deploy/compose.yaml ||
  ! grep -Fqx '      AGENTSHARK_OUTBOX_RETENTION: ${AGENTSHARK_OUTBOX_RETENTION}' deploy/compose.yaml ||
  ! grep -Fqx 'EXPOSE 8080 4318' deploy/Dockerfile; then
  echo "the Collector listener, retention settings, and published image port are incomplete" >&2
  exit 1
fi

if ! grep -Fqx '    image: ${POSTGRES_IMAGE}:${POSTGRES_VERSION}' deploy/compose.yaml || \
  ! grep -Fqx '      - "${AGENTSHARK_DATABASE_BIND}:${AGENTSHARK_DATABASE_PORT}:5432"' deploy/compose.yaml; then
  echo "Compose must use the pinned PostgreSQL image and configurable loopback publication" >&2
  exit 1
fi

for script in \
  scripts/bootstrap-preview.sh \
  scripts/bootstrap-sdk.sh \
  scripts/verify-agentguard-sdk.sh \
  scripts/agentgateway-standalone.sh \
  scripts/gateway-observability-smoke.sh \
  scripts/release-e2e.sh \
  scripts/standalone-compose.sh \
  scripts/preview.sh \
  scripts/demo.sh \
  scripts/demo-smoke.sh \
  scripts/test-postgres.sh; do
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
  scripts/bootstrap-preview.sh \
  scripts/bootstrap-sdk.sh \
  scripts/verify-agentguard-sdk.sh \
  scripts/release-e2e.sh \
  scripts/demo.sh \
  scripts/demo-smoke.sh \
  scripts/test-postgres.sh

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

if rg -n '/node_modules/|node_modules%2F' \
  docs/release/sbom.spdx.json docs/release/dependency-licenses.md; then
  echo "release artifacts contain an npm installation path instead of a package name" >&2
  exit 1
fi

agentguard_revision="$(sed -n 's/^AGENTGUARD_GIT_REVISION=//p' deploy/versions.env)"
if ! AGENTGUARD_SBOM_REVISION="$agentguard_revision" node -e '
  const document = JSON.parse(require("node:fs").readFileSync("docs/release/sbom.spdx.json", "utf8"));
  const revision = process.env.AGENTGUARD_SBOM_REVISION;
  const pinned = document.packages.some((item) => item.SPDXID === "SPDXRef-Upstream-AgentGuard" && item.versionInfo === revision && item.downloadLocation.endsWith(revision));
  const sdkDependency = document.relationships.some((item) => item.spdxElementId === "SPDXRef-AgentsharkX-Python-SDK-0.1.0" && item.relationshipType === "DEPENDS_ON" && item.relatedSpdxElement === "SPDXRef-Upstream-AgentGuard");
  if (!pinned || !sdkDependency) process.exit(1);
'; then
  echo "SBOM must pin the full AgentGuard revision and relate it to the Python SDK" >&2
  exit 1
fi

echo "repository invariants: ok"
