# Preview release evidence

Phase 14 release artifacts:

- `sbom.spdx.json`: SPDX 2.3 npm build/runtime inventory, reviewed BFF/Collector
  Go module graph, pinned local Python SDK runtime resolution, purls, dependency
  relationships, and pinned separate services;
- `dependency-licenses.md`: exact npm lockfile declarations, verified Go module
  and Python distribution licenses, and the separate-process/runtime SDK
  license boundaries;
- `security-scan.md`: Go vet, production npm audit, credential-pattern, browser
  bundle boundary, and non-root runtime result.

Regenerate the deterministic inventory with `make sbom` and rerun the live
checks with `make security-scan`. SBOM generation reads only checked-in lock and
module/constraint metadata; it does not download packages. These artifacts do not replace
registry-side scanning of the separately deployed pinned agentgateway,
AgentGuard, and PostgreSQL images.

`make release-e2e` builds the release image, applies the schema with its
migration-only entry point, and starts the real BFF and Collector against the
same isolated PostgreSQL instance with BFF auto-migration disabled. It sends an
authenticated OTLP/HTTP protobuf Trace, verifies its Span and summary rows
before and after a Collector restart, then restarts the BFF, verifies persisted
Gateway and Guard Audit rows, and resumes SSE from the pre-restart
`Last-Event-ID` without a reset.
