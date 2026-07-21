# AgentsharkX repository instructions

These rules apply to the whole repository.

## Before changing code

1. Read `README.md`, `docs/architecture.md`, `docs/capability-matrix.md`, and
   `docs/upstream-compatibility.md`.
2. Check `git status` and preserve user-owned changes.
3. Work on only the requested phase from
   `Agentshark_New_Repository_Codex_Execution_Plan.md`.
4. Check `api/upstream-contracts/` before using any upstream field. Mark an
   unverified field or endpoint `unverified`; never infer a schema.

## Product constraints

- Keep Home, Connect, Trust, Protect, and Audit as the product information
  architecture. System is a supporting area, not a fifth capability layer.
- AgentsharkX connects only to upstream management planes. It must not proxy
  agent business traffic.
- Do not add task inference, task graphs, replay, payload vaults, framework
  hooks, a database, or a new policy/guardrail engine.
- Preserve `source`, upstream IDs, scope, phase, and raw-detail references when
  normalizing data.
- Correlate sources only through an identical, explicitly verified trace or
  session identifier. Time proximity is never evidence.
- A failure in one upstream must not make the other upstream unavailable.
- Never log or return API keys, authorization headers, complete prompts, or
  unredacted sensitive payloads.

## Implementation and tests

- OpenAPI is the sole contract for the AgentsharkX BFF.
- Add or update tests with implementation changes.
- Bind UI motion to real state or clearly labeled mock state and honor
  `prefers-reduced-motion`.
- Keep all default image references pinned; `latest` is forbidden.
- Run `make verify` before every commit. Run the phase-specific checks in the
  execution plan as they become available.
- Update the capability matrix and compatibility notes when an adapter changes.

## Publishing

Create one focused commit per implementation round. Do not push or create a PR
unless the user explicitly requests it.
