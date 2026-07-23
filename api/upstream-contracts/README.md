# Upstream contract evidence

These files are sanitized response samples captured from running containers or,
where explicitly labelled `populated shape`, minimized from exact pinned-source
response builders/tests. They are not a substitute for capability probing and
freeze verified adapter contracts.

- `agentgateway/`: release `v1.3.1`, revision
  `dbaaf7ed73671e7aec9195e35e7f726c0b14b84a`.
- `agentguard/`: main snapshot (package version `2.1`), revision
  `4b755fb4a4a2763b7e817b3d0220fe5c22187b59`.

Dynamic timestamps and request IDs are omitted. API keys and authorization
headers are always represented as `<redacted>` and must never be committed.
Empty arrays are real initial-state responses. Files ending in
`unconfigured.response.json` preserve a verified degraded response rather than
claiming the capability is empty.
