# Upstream contract evidence

These files are sanitized response samples captured from running containers,
not invented fixtures and not a substitute for capability probing. They freeze
the Phase 0 baseline for adapter tests.

- `agentgateway/`: release `v1.3.1`, revision
  `dbaaf7ed73671e7aec9195e35e7f726c0b14b84a`.
- `agentguard/`: release `v2.1`, revision
  `6f95deb9f405eca41efb6cc58ccee5b1791c7b03`.

Dynamic timestamps and request IDs are omitted. API keys and authorization
headers are always represented as `<redacted>` and must never be committed.
Empty arrays are real initial-state responses. Files ending in
`unconfigured.response.json` preserve a verified degraded response rather than
claiming the capability is empty.
