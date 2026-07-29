#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
versions_file="$root_dir/deploy/versions.env"
target="$root_dir/third_party/AgentGuard"
source_url="https://github.com/WhitzardAgent/AgentGuard.git"
revision="$(sed -n 's/^AGENTGUARD_GIT_REVISION=//p' "$versions_file")"

if [[ ! "$revision" =~ ^[[:xdigit:]]{40}$ ]]; then
  echo "deploy/versions.env does not contain a full AgentGuard revision" >&2
  exit 1
fi

if [[ -d "$target/.git" ]]; then
  current="$(git -C "$target" rev-parse HEAD)"
  if [[ "$current" != "$revision" ]]; then
    echo "third_party/AgentGuard is at $current, expected $revision" >&2
    echo "move that checkout aside before running sdk-bootstrap again" >&2
    exit 1
  fi
elif [[ -e "$target" ]]; then
  echo "third_party/AgentGuard exists but is not a Git checkout; move it aside first" >&2
  exit 1
else
  mkdir -p "$(dirname "$target")"
  git clone --filter=blob:none --no-checkout "$source_url" "$target"
  git -C "$target" fetch --depth=1 origin "$revision"
  git -C "$target" checkout --detach "$revision"
fi

test "$(git -C "$target" rev-parse HEAD)" = "$revision"
test -s "$target/LICENSE"
test -s "$target/pyproject.toml"

echo "Pinned AgentGuard checkout ready at third_party/AgentGuard ($revision)."
echo "Install locally with: python -m pip install -c ./sdk/python/constraints.txt -e ./third_party/AgentGuard -e './sdk/python[dev]'"
