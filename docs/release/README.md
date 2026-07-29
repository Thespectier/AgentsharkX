# Preview release evidence

Phase 13 release artifacts:

- `sbom.spdx.json`: SPDX 2.3 npm build/runtime inventory, reviewed Go runtime
  module graph, purls, dependency relationships, and pinned separate services;
- `dependency-licenses.md`: exact npm lockfile declarations, verified Go module
  licenses, and the separate-process runtime license boundary;
- `security-scan.md`: Go vet, production npm audit, credential-pattern, browser
  bundle boundary, and non-root runtime result.

Regenerate the deterministic inventory with `make sbom` and rerun the live
checks with `make security-scan`. SBOM generation reads only checked-in lock and
module metadata; it does not download modules. These artifacts do not replace
registry-side scanning of the separately deployed pinned agentgateway,
AgentGuard, and PostgreSQL images.

`make release-e2e` also restarts the real BFF against the same isolated
PostgreSQL instance, reauthenticates, verifies persisted Gateway and Guard Audit
rows, and resumes SSE from the pre-restart `Last-Event-ID` without a reset.
