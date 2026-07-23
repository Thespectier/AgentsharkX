# 10-minute preview quickstart

This path starts the pinned agentgateway release and AgentGuard main snapshot
plus the AgentsharkX `0.7.0-preview` image, then emits one real AgentGuard
event. The default integrated topology supports Linux x86_64/arm64 and macOS
arm64, and requires Docker with Compose v2, `curl`, `jq`, a SHA-256 utility,
OpenSSL, Python 3.11+, and Git. Agentgateway runs as a verified host-native
binary; the other services remain in Compose.

## 1. Create local credentials

```bash
make preview-bootstrap
```

The command creates an ignored `.env` with mode `0600` and two random 24-byte
values encoded as 48 hex characters. It also creates an ignored
`.agentgateway.env` for provider credentials. It never replaces an existing
file or prints credentials. Review every `*_BIND` value before changing it from
loopback.

## 2. Start the preview

```bash
make preview-up
make preview-status
```

The first start downloads the exact release binary to ignored `.cache/`,
verifies its pinned SHA-256 digest and version/revision, then runs it directly
as the checkout user. AgentsharkX and both AgentGuard services remain
containerized. A gateway LLM/MCP business listener therefore appears directly
on its configured host port without an extra Docker mapping.

Open <http://localhost:8080>. Retrieve the local administrator token from
`.env`, enter it once in the login screen, and then open **System**. Both source
cards should report healthy. The browser stores only an `HttpOnly` session
cookie; a page reload obtains a fresh in-memory CSRF value from the authenticated
session endpoint.

Docker Desktop automatically uses `host.docker.internal`; native Linux Docker
uses host networking. Use `make preview-container-up` if neither connector is
available. That fallback runs agentgateway in its pinned container and requires
explicit Compose port mappings for additional business listeners.

## 3. Emit the first real event

Install the exact pinned AgentGuard client in a disposable virtual environment:

```bash
python3 -m venv .venv-quickstart
.venv-quickstart/bin/pip install \
  'agentguard @ git+https://github.com/WhitzardAgent/AgentGuard.git@4b755fb4a4a2763b7e817b3d0220fe5c22187b59'
```

Run the repository example using the same AgentGuard API key as Compose:

```bash
set -a
. ./.env
set +a
AGENTGUARD_SERVER_URL=http://127.0.0.1:38080 \
AGENTGUARD_API_KEY="$AGENTGUARD_ADMIN_TOKEN" \
  .venv-quickstart/bin/python examples/agentguard_minimal.py
```

Within three seconds, **Audit → Security events** shows the source-labelled
AgentGuard tool event and **Trust → Agents** shows the explicit quickstart agent.
No prompt, tool arguments, session key, API key, or raw sensitive response is
returned to the browser.

## 4. Stop

```bash
make preview-down
```

`.env` and `.venv-quickstart` remain local. AgentShark's event ring, scan jobs,
and rule-check tokens are in memory and disappear when the container stops.
See [Agent integration](agent-integration.md) for framework adapters and
[Troubleshooting](troubleshooting.md) if either source is degraded.
