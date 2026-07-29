#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
revision="$(sed -n 's/^AGENTGUARD_GIT_REVISION=//p' "$root_dir/deploy/versions.env")"
python_image="python:3.12.11-slim@sha256:47ae396f09c1303b8653019811a8498470603d7ffefc29cb07c88f1f8cb3d19f"
temp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup EXIT

git clone --quiet https://github.com/WhitzardAgent/AgentGuard.git "$temp_dir/AgentGuard"
git -C "$temp_dir/AgentGuard" checkout --quiet "$revision"
test "$(git -C "$temp_dir/AgentGuard" rev-parse HEAD)" = "$revision"
mkdir -p "$temp_dir/sdk/src"
cp -a \
  "$root_dir/sdk/python/pyproject.toml" \
  "$root_dir/sdk/python/constraints.txt" \
  "$root_dir/sdk/python/README.md" \
  "$temp_dir/sdk/"
cp -a "$root_dir/sdk/python/src/agentshark" "$temp_dir/sdk/src/"

docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e HOME=/tmp \
  -w /tmp \
  -v "$temp_dir/AgentGuard:/agentguard" \
  -v "$temp_dir/sdk:/sdk" \
  "$python_image" \
  sh -c 'python -m venv /tmp/agentshark-contract && /tmp/agentshark-contract/bin/python -m pip install --disable-pip-version-check --quiet -c /sdk/constraints.txt -e /agentguard -e "/sdk[dev]" && /tmp/agentshark-contract/bin/python - <<"PY"
import inspect

from agentguard import Guard, Principal

principal = Principal(session_id="contract-session", agent_id="contract-agent")
guard = Guard(fail_open=True).start(principal=principal)
assert callable(guard.attach_langchain)
assert callable(guard.close)
assert "agent" in inspect.signature(guard.attach_langchain).parameters
native_guard = guard._guard
assert native_guard is not None
assert callable(native_guard._remote.fetch_snapshot)
assert native_guard._enforcer.remote is native_guard._remote
guard.close()
print("AgentGuard editable install and public compatibility facade: ok")
PY'
