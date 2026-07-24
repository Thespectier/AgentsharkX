import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Database, ExternalLink, ServerCog, ShieldCheck } from "lucide-react";

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
import { formatTimeWithZone } from "../lib/format";
import { useI18n } from "../lib/i18n";

export function SystemPage() {
  const { t } = useI18n();
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
  const diagnostics = useQuery({
    queryKey: ["system-diagnostics"],
    queryFn: ({ signal }) => requestOperation("getSystemDiagnostics", signal),
    retry: false,
  });

  if (health.isLoading || capabilities.isLoading || diagnostics.isLoading) {
    return <PageSkeleton label="Probing upstream capabilities" />;
  }
  if (
    health.isError ||
    capabilities.isError ||
    diagnostics.isError ||
    !health.data ||
    !capabilities.data ||
    !diagnostics.data
  ) {
    return (
      <PageFrame>
        <PageHeader
          description="Diagnostics support the four product workspaces; System is not a fifth capability layer."
          eyebrow="System / Diagnostics"
          title="Sources, versions & capabilities"
        />
        <ErrorState
          description={formatError(health.error ?? capabilities.error ?? diagnostics.error)}
          onRetry={() => {
            void health.refetch();
            void capabilities.refetch();
            void diagnostics.refetch();
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
          const issue = diagnostics.data.data.issues.find(
            (candidate) => candidate.source === source.source,
          );
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
                  <strong>{source.version ?? t("Version unavailable")}</strong>
                  <span>
                    {source.latencyMs === null ? t("No latency sample") : `${source.latencyMs} ms`}
                  </span>
                </div>
              </div>
              <ul className="diagnostic-list">
                <li>
                  <StatusOrb status={source.status} /> {t("Live management probe")}:{" "}
                  {t(source.status)}
                </li>
                <li>
                  <Database size={14} /> {t("Checked")} {formatTimeWithZone(source.checkedAt)}
                </li>
                {source.message ? <li>{t(source.message)}</li> : null}
              </ul>
              {issue ? (
                <div className="recovery-guide" role="status">
                  <div className="recovery-guide__summary">
                    <AlertTriangle aria-hidden="true" size={15} />
                    <strong>{t(issue.summary)}</strong>
                  </div>
                  <ol>
                    {issue.checks.map((check) => (
                      <li key={check}>{t(check)}</li>
                    ))}
                  </ol>
                  <a href={issue.documentationPath} rel="noreferrer" target="_blank">
                    {t("Troubleshooting guide")} <ExternalLink aria-hidden="true" size={12} />
                  </a>
                </div>
              ) : null}
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
                <strong>{t(capability.reason ?? capability.id)}</strong>
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
