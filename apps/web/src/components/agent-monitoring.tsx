import { useQuery } from "@tanstack/react-query";
import { Activity, Bot, Clock3 } from "lucide-react";

import type { TrustAgent } from "../generated/api-client";
import { formatCount, formatDateTimeWithZone } from "../lib/format";
import { formatError, getScenario, requestOperation } from "../lib/api";
import { useI18n } from "../lib/i18n";
import { Card, CardHeader, EmptyState, ErrorState, StatusBadge } from "./ui";

export function AgentMonitoringOverview({ compact = false }: { compact?: boolean }) {
  const { t } = useI18n();
  const query = useQuery({
    queryKey: ["monitored-agents", getScenario()],
    queryFn: ({ signal }) =>
      requestOperation("listAgents", { signal, query: { limit: compact ? 6 : 100 } }),
    retry: false,
  });

  if (query.isError || (!query.isLoading && !query.data)) {
    return (
      <Card className="agent-monitoring">
        <CardHeader
          description="Agent monitoring data could not be loaded."
          title="Agent monitoring overview"
        />
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      </Card>
    );
  }

  const page = query.data?.data;
  const agents = page?.items ?? [];
  const running = reportedRunningCount(agents);

  return (
    <section aria-label={t("Agent monitoring overview")} className="agent-monitoring">
      <div className="agent-monitoring__metrics">
        <Card as="article" className="agent-monitoring__metric">
          <span className="agent-monitoring__metric-icon">
            <Bot aria-hidden="true" size={18} />
          </span>
          <div>
            <p>{t("Monitored agents")}</p>
            <strong>{query.isLoading ? "..." : formatCount(page?.total ?? 0)}</strong>
            <span>{t("Explicitly reported identities")}</span>
          </div>
        </Card>
        <Card as="article" className="agent-monitoring__metric">
          <span className="agent-monitoring__metric-icon agent-monitoring__metric-icon--active">
            <Activity aria-hidden="true" size={18} />
          </span>
          <div>
            <p>{t("Running agents")}</p>
            <strong>
              {query.isLoading ? "..." : running === null ? "—" : formatCount(running)}
            </strong>
            <span>
              {t(running === null ? "Runtime state is not reported" : "Explicit running status")}
            </span>
          </div>
        </Card>
      </div>

      <Card className="agent-monitoring__list">
        <CardHeader
          description="Names, runtime state, sessions, and activity are shown only when reported by the management API."
          title="Monitored agent status"
        />
        {query.isLoading ? (
          <div
            aria-label={t("Loading monitored agents")}
            className="agent-monitoring__loading"
            role="status"
          >
            {t("Loading monitored agents...")}
          </div>
        ) : agents.length ? (
          <div className="agent-status-list">
            {agents.map((agent) => (
              <article className="agent-status-row" key={agent.id}>
                <span className="agent-status-row__icon">
                  <Bot aria-hidden="true" size={16} />
                </span>
                <div className="agent-status-row__identity">
                  <strong>{agent.name}</strong>
                  <span>{agent.framework ?? t("Framework not reported")}</span>
                </div>
                <div className="agent-status-row__sessions">
                  <span>{t("Sessions")}</span>
                  <strong>{formatCount(agent.sessions)}</strong>
                </div>
                <div className="agent-status-row__activity">
                  <Clock3 aria-hidden="true" size={13} />
                  <span>
                    {agent.lastActive
                      ? formatDateTimeWithZone(agent.lastActive)
                      : t("Activity not reported")}
                  </span>
                </div>
                <StatusBadge status={agent.status} />
              </article>
            ))}
          </div>
        ) : (
          <EmptyState
            compact
            description="No monitored Agent identity has been reported yet."
            title="No monitored agents"
          />
        )}
        {!compact && page && page.total > agents.length ? (
          <p className="resource-note">
            {t("Showing {shown} of {total} monitored agents", {
              shown: agents.length,
              total: page.total,
            })}
          </p>
        ) : null}
      </Card>
    </section>
  );
}

export function reportedRunningCount(_agents: TrustAgent[]): number | null {
  // The current OpenAPI contract only permits `unknown`; do not infer runtime state from activity.
  return null;
}
