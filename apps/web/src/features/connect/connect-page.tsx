import { useQuery } from "@tanstack/react-query";
import { Cable, CheckCircle2, Network, Route, Waypoints } from "lucide-react";

import { AgentMonitoringOverview } from "../../components/agent-monitoring";
import { PageFrame, useDocumentTitle, useWorkspaceSection } from "../../components/workspace";
import {
  Card,
  CardHeader,
  DefinitionList,
  ErrorState,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  StatusBadge,
  StatusOrb,
} from "../../components/ui";
import type { ConnectSummary } from "../../generated/api-client";
import { formatCount, formatTimeWithZone } from "../../lib/format";
import { formatError, getScenario, requestOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { LlmManager } from "./llm-manager";
import { McpManager } from "./mcp-manager";
import { TrafficManager } from "./traffic-manager";

export function ConnectPage() {
  const section = useWorkspaceSection("connect", "overview");
  const heading = connectHeadings[section] ?? connectHeadings.overview;
  useDocumentTitle(heading.title);
  const scenario = getScenario();
  const summary = useQuery({
    queryKey: ["connect-summary", scenario],
    queryFn: ({ signal }) => requestOperation("getConnectSummary", signal),
    retry: false,
  });
  if (summary.isLoading) return <PageSkeleton label="Loading connection resources" />;
  if (summary.isError || !summary.data)
    return (
      <PageFrame>
        <PageHeader
          description="Agents, providers, models, MCP tools, listeners, and routes."
          eyebrow="Connect / Overview"
          title="Connection data unavailable"
        />
        <ErrorState
          description={formatError(summary.error)}
          onRetry={() => void summary.refetch()}
        />
      </PageFrame>
    );
  const { data, meta } = summary.data;
  return (
    <PageFrame>
      <PageHeader
        description={heading.description}
        eyebrow={`Connect / ${heading.label}`}
        title={heading.title}
      />
      <PartialBanner meta={meta} />
      {section === "overview" ? (
        <ConnectOverview summary={data} fetchedAt={meta.fetchedAt} />
      ) : null}
      {section === "agents" ? <AgentMonitoringOverview /> : null}
      {section === "llm" ? <LlmManager /> : null}
      {section === "mcp" ? <McpManager /> : null}
      {section === "traffic" ? <TrafficManager /> : null}
    </PageFrame>
  );
}

const connectHeadings: Record<string, { label: string; title: string; description: string }> = {
  overview: {
    label: "Overview",
    title: "Connection overview",
    description: "Review connection health, configured destinations, and recent request activity.",
  },
  agents: {
    label: "Agents",
    title: "Monitored Agents",
    description: "Review Agent identities and activity explicitly reported by the monitoring API.",
  },
  llm: {
    label: "LLM / Provider",
    title: "LLM / Provider",
    description:
      "Manage verified provider, model, credential reference, and virtual model settings.",
  },
  mcp: {
    label: "MCP / Tools",
    title: "MCP / Tools",
    description: "Manage MCP targets, tool exposure, and verified connection settings.",
  },
  traffic: {
    label: "Traffic",
    title: "Traffic configuration",
    description: "Manage verified listeners and routes without proxying Agent business traffic.",
  },
};

function ConnectOverview({ summary, fetchedAt }: { summary: ConnectSummary; fetchedAt: string }) {
  const { t } = useI18n();
  const icons = [Cable, Route, Network, Waypoints];
  const analytics = summary.analytics;
  return (
    <>
      <div className="summary-grid">
        {summary.counts.map((item, index) => {
          const Icon = icons[index] ?? Cable;
          return (
            <Card className="summary-card" key={item.id}>
              <span className="summary-card__icon">
                <Icon size={18} />
              </span>
              <div>
                <p>{t(item.label)}</p>
                <strong>{item.value === null ? t("Unavailable") : formatCount(item.value)}</strong>
                <span>
                  <CheckCircle2 size={12} /> {t(item.status)}
                </span>
              </div>
            </Card>
          );
        })}
      </div>
      <div className="content-grid">
        <Card>
          <CardHeader
            action={
              <span className="fetched-at">
                {t("Fetched")} {formatTimeWithZone(fetchedAt)}
              </span>
            }
            description="Runtime and configuration were checked independently by the BFF."
            title="Connection status"
          />
          <div className="connection-check">
            <StatusOrb status={summary.health.status} />
            <div>
              <strong>{t(summary.health.status)}</strong>
              <span>
                {summary.health.version ?? t("Version unavailable")} ·{" "}
                {summary.health.latencyMs ?? "—"} ms
              </span>
            </div>
          </div>
        </Card>
        <Card>
          <CardHeader
            action={<StatusBadge status={analytics.status} />}
            description="Last 60 minutes, derived only from the verified request-log analytics contract."
            title="Analytics summary"
          />
          {analytics.status === "available" ? (
            <DefinitionList
              items={[
                { label: "Requests", value: formatNullable(analytics.requests) },
                { label: "Total tokens", value: formatNullable(analytics.totalTokens) },
                {
                  label: "Estimated cost",
                  value: analytics.cost === null ? "Not provided" : `$${analytics.cost.toFixed(4)}`,
                },
                { label: "Buckets", value: analytics.buckets.length },
              ]}
            />
          ) : (
            <p className="resource-note">{t(analytics.reason ?? "Analytics is unavailable.")}</p>
          )}
        </Card>
      </div>
    </>
  );
}

function formatNullable(value: number | null) {
  return value === null ? "Not provided" : formatCount(value);
}
