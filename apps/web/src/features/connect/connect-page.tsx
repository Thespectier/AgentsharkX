import { useQuery } from "@tanstack/react-query";
import { Cable, CheckCircle2, Network, RefreshCw, Route, Waypoints } from "lucide-react";

import { PageFrame, useWorkspaceSection } from "../../components/workspace";
import {
  Button,
  Card,
  CardHeader,
  DefinitionList,
  ErrorState,
  ExternalButton,
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
  const scenario = getScenario();
  const summary = useQuery({
    queryKey: ["connect-summary", scenario],
    queryFn: ({ signal }) => requestOperation("getConnectSummary", signal),
    retry: false,
  });
  if (summary.isLoading) return <PageSkeleton label="Loading gateway resources" />;
  if (summary.isError || !summary.data)
    return (
      <PageFrame>
        <PageHeader
          description="agentgateway listeners, providers, models, MCP targets, and routes."
          eyebrow="Connect / agentgateway"
          title="Gateway data unavailable"
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
        actions={
          data.links.rawConfig || data.links.console ? (
            <ExternalButton href={data.links.rawConfig ?? data.links.console!}>
              Configure agentgateway
            </ExternalButton>
          ) : undefined
        }
        description="Manage verified LLM, MCP, listener, and route configuration through the agentgateway management plane."
        eyebrow="Connect / agentgateway"
        title="Connect agents to every destination"
      />
      <PartialBanner meta={meta} />
      {section === "overview" ? (
        <ConnectOverview summary={data} fetchedAt={meta.fetchedAt} />
      ) : null}
      {section === "llm" ? <LlmManager /> : null}
      {section === "mcp" ? <McpManager /> : null}
      {section === "traffic" ? <TrafficManager /> : null}
      {section === "setup" ? <SetupView /> : null}
    </PageFrame>
  );
}

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
      <NativeLinks links={summary.links} />
    </>
  );
}

function SetupView() {
  const { t } = useI18n();
  const query = useQuery({
    queryKey: ["connect-setup", getScenario()],
    queryFn: ({ signal }) => requestOperation("verifyGatewaySetup", signal),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Verifying agentgateway management access" />;
  if (query.isError || !query.data)
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );
  const setup = query.data.data;
  return (
    <div className="setup-grid">
      <Card elevated>
        <CardHeader
          description="Live BFF checks against /api/runtime and /api/config."
          title="Management verification"
        />
        <div className="connection-check">
          <StatusOrb status={setup.status} />
          <div>
            <strong>
              {t(setup.configurationReadable ? "Connection verified" : "Configuration unreadable")}
            </strong>
            <span>
              {setup.version ?? t("Version unavailable")} · {setup.latencyMs ?? "—"} ms ·{" "}
              {t("Checked")} {formatTimeWithZone(setup.checkedAt)}
            </span>
          </div>
        </div>
        {setup.message ? <p className="resource-note">{setup.message}</p> : null}
        <Button onClick={() => void query.refetch()} variant="secondary">
          <RefreshCw size={14} /> Run check
        </Button>
      </Card>
      <Card>
        <CardHeader
          description="Advanced editors stay in the pinned agentgateway console."
          title="Native console tools"
        />
        <NativeLinks links={setup.links} compact />
      </Card>
    </div>
  );
}

function NativeLinks({
  links,
  compact = false,
}: {
  links: { rawConfig?: string; cel?: string; llmPlayground?: string; mcpPlayground?: string };
  compact?: boolean;
}) {
  const { t } = useI18n();
  const values = [
    ["Raw Config", links.rawConfig],
    ["CEL Playground", links.cel],
    ["LLM Playground", links.llmPlayground],
    ["MCP Playground", links.mcpPlayground],
  ] as const;
  const available = values.flatMap(([label, href]) => (href ? [{ label, href }] : []));
  if (!available.length)
    return compact ? (
      <p className="resource-note">{t("No validated console URL is configured.")}</p>
    ) : null;
  return (
    <Card className={compact ? "native-links native-links--compact" : "native-links"}>
      {!compact ? (
        <CardHeader
          description="Use upstream-native tools for advanced editing and testing."
          title="Open in agentgateway"
        />
      ) : null}
      <div className="native-links__actions">
        {available.map(({ label, href }) => (
          <ExternalButton href={href} key={label}>
            {label}
          </ExternalButton>
        ))}
      </div>
    </Card>
  );
}

function formatNullable(value: number | null) {
  return value === null ? "Not provided" : formatCount(value);
}
