import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, CheckCircle2, ShieldCheck, Sparkles, TerminalSquare } from "lucide-react";

import { ActivityRail, LiveFlow, RequestTrendChart } from "../../motion/dashboard-motion";
import { formatError, getScenario, isMockMode, requestOperation } from "../../lib/api";
import { useLiveEvents } from "../../lib/use-live-events";
import {
  Button,
  Card,
  CardHeader,
  ErrorState,
  MetricCard,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SourceBadge,
  StatusOrb,
  TimelineStep,
} from "../../components/ui";
import { PageFrame } from "../../components/workspace";

export function HomePage() {
  const scenario = getScenario();
  const query = useQuery({
    queryKey: ["overview", scenario],
    queryFn: ({ signal }) => requestOperation("getOverview", signal),
    retry: false,
  });
  const live = useLiveEvents(query.isSuccess && scenario !== "empty");

  if (query.isLoading) return <PageSkeleton label="Loading runtime posture" />;
  if (query.isError || !query.data) {
    return (
      <PageFrame>
        <PageHeader
          description="Source-scoped health, gateway traffic, runtime decisions, and actions requiring human attention."
          eyebrow="Home / Runtime posture"
          title="Control plane unavailable"
        />
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      </PageFrame>
    );
  }

  const { data, meta } = query.data;
  if (!data.setup.complete) {
    return (
      <PageFrame>
        <PageHeader
          description="Connect both management planes and send one request before AgentsharkX renders operational charts."
          eyebrow="Home / First run"
          title="Bring your control plane online"
        />
        <PartialBanner meta={meta} />
        <Card className="onboarding-card" elevated>
          <div className="onboarding-card__visual">
            <span className="onboarding-logo">
              <Sparkles size={28} />
            </span>
            <div>
              <p className="eyebrow">Three-step setup</p>
              <h2>From zero to the first verified event</h2>
              <p>
                No empty charts and no invented traffic. The console activates each surface only
                after its source responds.
              </p>
            </div>
          </div>
          <div className="onboarding-card__steps">
            {data.setup.steps.map((step, index) => (
              <TimelineStep
                complete={step.complete}
                description={step.command}
                key={step.id}
                label={step.label}
                last={index === data.setup.steps.length - 1}
              />
            ))}
          </div>
          <div className="onboarding-card__actions">
            <Link
              className="button button--primary button--md"
              params={{ section: "setup" }}
              search={true}
              to="/connect/$section"
            >
              Open setup <ArrowRight size={15} />
            </Link>
            <span>
              <ShieldCheck size={15} /> Credentials stay in the BFF
            </span>
          </div>
        </Card>
      </PageFrame>
    );
  }

  if (data.mode === "health-only") {
    return (
      <PageFrame>
        <PageHeader
          description="The secure BFF is connected and preserving each upstream's independent health and capability state."
          eyebrow="Home / Phase 2 foundation"
          title="Management planes connected"
        >
          <div className="health-strip">
            {data.health.map((item) => (
              <div className="health-strip__item" key={item.source}>
                <StatusOrb label={`${item.label} ${item.status}`} status={item.status} />
                <div>
                  <SourceBadge source={item.source} />
                  <span>
                    {item.version ?? "version unavailable"} · {item.latencyMs ?? "—"} ms
                  </span>
                </div>
              </div>
            ))}
          </div>
        </PageHeader>
        <PartialBanner meta={meta} />
        <div className="phase-foundation-grid">
          <Card elevated>
            <CardHeader
              description="Health events are normalized without collecting prompts, authorization headers, or raw upstream payloads."
              title="Secure health stream"
            />
            <div className="foundation-status">
              <StatusOrb status={live.status === "live" ? "healthy" : "connecting"} />
              <strong>{live.status === "live" ? "SSE connected" : "Connecting to SSE"}</strong>
              <span>Heartbeat and source health changes only</span>
            </div>
          </Card>
          <Card elevated>
            <CardHeader
              description="Traffic, decisions, approvals, and audit arrays remain empty until their verified integration phases."
              title="No fabricated operations"
            />
            <div className="foundation-status">
              <ShieldCheck size={20} />
              <strong>BFF boundary active</strong>
              <span>Use System to inspect live capability probes</span>
            </div>
          </Card>
        </div>
        <div className="mock-footnote">
          <TerminalSquare size={14} />
          {isMockMode()
            ? "This view is driven by the labelled Phase 1 fixture."
            : "This view is driven by authenticated Phase 2 BFF responses."}
        </div>
      </PageFrame>
    );
  }

  const activity = [...live.events, ...data.events].slice(0, 12);
  const security = activity.filter(
    (event) => event.source === "agentguard" && event.severity !== "info",
  );
  return (
    <PageFrame>
      <PageHeader
        actions={
          <>
            <Link
              className="button button--secondary button--md"
              params={{ section: "security-events" }}
              search={true}
              to="/audit/$section"
            >
              View security events
            </Link>
            <Link
              className="button button--primary button--md"
              params={{ section: "approvals" }}
              search={true}
              to="/protect/$section"
            >
              Review approvals <ArrowRight size={15} />
            </Link>
          </>
        }
        description="Source-scoped health, live gateway traffic, runtime decisions, and actions requiring human attention."
        eyebrow="Home / Runtime posture"
        title="Good afternoon. Your agents are in control."
      >
        <div className="health-strip">
          {data.health.map((item) => (
            <div className="health-strip__item" key={item.source}>
              <StatusOrb
                label={`${item.label} ${item.status}`}
                status={
                  scenario === "partial" && item.source === "agentguard" ? "degraded" : item.status
                }
              />
              <div>
                <SourceBadge source={item.source} />
                <span>
                  {scenario === "partial" && item.source === "agentguard"
                    ? "Probe timed out · cached view"
                    : `${item.version} · ${item.latencyMs} ms`}
                </span>
              </div>
            </div>
          ))}
          <span className="health-strip__sync">
            <CheckCircle2 size={14} /> Refreshed 8s ago
          </span>
        </div>
      </PageHeader>
      <PartialBanner meta={meta} />

      <div className="home-flow">
        <LiveFlow events={live.events} status={live.status} />
      </div>
      <div className="metric-grid">
        {data.metrics.map((metric) => (
          <MetricCard key={metric.id} metric={metric} />
        ))}
      </div>

      <div className="content-grid content-grid--wide">
        <Card className="chart-card">
          <CardHeader
            action={
              <div className="chart-legend">
                <span>
                  <i className="legend-dot legend-dot--blue" />
                  Requests
                </span>
                <span>
                  <i className="legend-dot legend-dot--danger" />
                  Denied
                </span>
              </div>
            }
            description="Gateway requests with AgentGuard decisions shown independently."
            title="Traffic & decisions"
          />
          <RequestTrendChart data={data.trend} />
        </Card>
        <Card className="security-queue">
          <CardHeader
            action={
              <Link
                className="text-link"
                params={{ section: "approvals" }}
                search={true}
                to="/protect/$section"
              >
                Open queue <ArrowRight size={13} />
              </Link>
            }
            description="Latest deny, human-check, and audit findings."
            title="Security queue"
          />
          {security.length ? (
            <ActivityRail events={security} limit={4} />
          ) : (
            <div className="mini-empty">
              <ShieldCheck size={24} />
              <p>No security events in this range.</p>
            </div>
          )}
        </Card>
      </div>

      <Card className="activity-card">
        <CardHeader
          action={
            <span className="live-caption">
              <StatusOrb status={live.status === "live" ? "healthy" : "connecting"} />
              {live.status === "live"
                ? `${isMockMode() ? "Mock" : "Live"} SSE connected`
                : "Connecting"}
            </span>
          }
          description="Unified presentation only. No task or time-window correlation is implied."
          title="Unified activity"
        />
        <ActivityRail events={activity} limit={6} />
      </Card>
      <div className="mock-footnote">
        <TerminalSquare size={14} />
        {isMockMode()
          ? "Dynamic elements on this page are driven by clearly labelled MSW REST and SSE fixtures."
          : "Dynamic elements on this page are driven by authenticated BFF REST and SSE responses."}
      </div>
    </PageFrame>
  );
}
