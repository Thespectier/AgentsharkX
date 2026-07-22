# Preview release evidence

Phase 7 release artifacts:

- `sbom.spdx.json`: SPDX 2.3 build/runtime dependency inventory;
- `dependency-licenses.md`: exact npm lockfile license declarations plus the
  separate-process upstream license boundary;
- `security-scan.md`: Go vet, production npm audit, credential-pattern, browser
  bundle boundary, and non-root runtime result.

Regenerate the deterministic inventory with `make sbom` and rerun the live
checks with `make security-scan`. These artifacts do not replace registry-side
scanning of the separately deployed pinned agentgateway and AgentGuard images.
