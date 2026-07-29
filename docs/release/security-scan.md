# Security scan result

Preview scan captured on 2026-07-29 from the pinned Phase 14 dependency graph.

| Check | Result |
| --- | --- |
| Go `go vet ./...` | Pass |
| Python SDK | Pass: 49 pytest, Ruff, strict mypy, pinned AgentGuard public API contract |
| npm production audit | Pass: info 0, low 0, moderate 0, high 0, critical 0 |
| Tracked-file credential patterns | Pass |
| Browser bundle secret boundary | Pass |
| Runtime identity | Non-root UID/GID `65532:65532`, verified by the container release gate |
| Release inventory | Pass: deterministic SPDX SBOM with 412 packages, 14 Go runtime modules, and 21 Python SDK runtime packages |

The npm result covers dependencies shipped into the AgentsharkX web build. The
Go check covers both the BFF and OTLP Collector; the Python inventory covers the
repository-local SDK's pinned runtime resolution. The separately
deployed PostgreSQL, agentgateway, and AgentGuard images retain their own
vulnerability-management and license obligations; operators must scan those
exact pinned images in their registry before production promotion.
