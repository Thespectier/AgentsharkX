import { useQuery } from "@tanstack/react-query";
import { Database, ServerCog, ShieldCheck } from "lucide-react";

import { PageFrame } from "../components/workspace";
import {
  Card,
  CardHeader,
  ErrorState,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SourceBadge,
  StatusBadge,
  StatusOrb,
} from "../components/ui";
import { formatError, requestOperation } from "../lib/api";

export function SystemPage() {
  const health = useQuery({
    queryKey: ["system-health"],
    queryFn: ({ signal }) => requestOperation("getSystemHealth", signal),
    retry: false,
  });
  const capabilities = useQuery({
    queryKey: ["system-capabilities"],
    queryFn: ({ signal }) => requestOperation("getCapabilities", signal),
    retry: false,
  });

  if (health.isLoading || capabilities.isLoading) {
    return <PageSkeleton label="Probing upstream capabilities" />;
  }
  if (health.isError || capabilities.isError || !health.data || !capabilities.data) {
    return (
      <PageFrame>
        <PageHeader
          description="Diagnostics support the four product workspaces; System is not a fifth capability layer."
          eyebrow="System / Diagnostics"
          title="Sources, versions & capabilities"
        />
        <ErrorState
          description={formatError(health.error ?? capabilities.error)}
          onRetry={() => {
            void health.refetch();
            void capabilities.refetch();
          }}
        />
      </PageFrame>
    );
  }

  return (
    <PageFrame>
      <PageHeader
        description="Diagnostics support the four product workspaces; System is not a fifth capability layer."
        eyebrow="System / Diagnostics"
        title="Sources, versions & capabilities"
      />
      <PartialBanner meta={health.data.meta} />
      <div className="source-card-grid">
        {health.data.data.map((source) => {
          const gateway = source.source === "agentgateway";
          return (
            <Card elevated key={source.source}>
              <CardHeader
                action={<StatusBadge status={source.status} />}
                description={
                  gateway ? "Standalone management plane" : "Runtime security control plane"
                }
                title={source.label}
              />
              <div className="system-source">
                <span
                  className={`system-source__icon${gateway ? "" : " system-source__icon--guard"}`}
                >
                  {gateway ? <ServerCog size={24} /> : <ShieldCheck size={24} />}
                </span>
                <div>
                  <SourceBadge source={source.source} />
                  <strong>{source.version ?? "Version unavailable"}</strong>
                  <span>
                    {source.latencyMs === null ? "No latency sample" : `${source.latencyMs} ms`}
                  </span>
                </div>
              </div>
              <ul className="diagnostic-list">
                <li>
                  <StatusOrb status={source.status} /> Live management probe: {source.status}
                </li>
                <li>
                  <Database size={14} /> Checked {new Date(source.checkedAt).toLocaleTimeString()}
                </li>
                {source.message ? <li>{source.message}</li> : null}
              </ul>
            </Card>
          );
        })}
      </div>
      <Card>
        <CardHeader
          action={
            <span className="fetched-at">
              <Database size={13} /> Probe based
            </span>
          }
          description="The UI hides, disables, or links out according to this registry. Version numbers alone never enable a feature."
          title="Capability registry"
        />
        <div className="capability-list">
          {capabilities.data.data.map((capability) => (
            <div key={capability.id}>
              <StatusOrb
                status={
                  capability.status === "supported"
                    ? "healthy"
                    : capability.status === "partial"
                      ? "degraded"
                      : "down"
                }
              />
              <div>
                <strong>{capability.reason ?? capability.id}</strong>
                <code>{capability.id}</code>
              </div>
              <SourceBadge source={capability.source} />
              <StatusBadge status={capability.status} />
            </div>
          ))}
        </div>
      </Card>
    </PageFrame>
  );
}
