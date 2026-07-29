# Security scan result

Preview scan captured on 2026-07-29 from the pinned Phase 13 dependency graph.

| Check | Result |
| --- | --- |
| Go `go vet ./...` | Pass |
| npm production audit | Pass: info 0, low 0, moderate 0, high 0, critical 0 |
| Tracked-file credential patterns | Pass |
| Browser bundle secret boundary | Pass |
| Runtime identity | Non-root UID/GID `65532:65532`, verified by the container release gate |
| Release inventory | Pass: deterministic SPDX SBOM with 382 packages and 6 Go runtime modules |

The npm result covers dependencies shipped into the AgentsharkX web build, and
the Go check includes the `pgx` PostgreSQL client dependency. The separately
deployed PostgreSQL, agentgateway, and AgentGuard images retain their own
vulnerability-management and license obligations; operators must scan those
exact pinned images in their registry before production promotion.
