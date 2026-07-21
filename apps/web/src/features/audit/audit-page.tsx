import { useQuery } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import {
  Activity,
  Braces,
  CheckCircle2,
  CircleSlash,
  Filter,
  ListFilter,
  Radio,
  ShieldAlert,
} from "lucide-react";
import { useCallback, useMemo, useRef } from "react";

import { RequestTrendChart } from "../../motion/dashboard-motion";
import { currentSection, PageFrame, WorkspaceTabs } from "../../components/workspace";
import {
  Button,
  Card,
  CardHeader,
  DataTable,
  DefinitionList,
  DetailDrawer,
  EmptyState,
  ErrorState,
  MetricCard,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SeverityBadge,
  SourceBadge,
  StatusBadge,
  type Column,
} from "../../components/ui";
import { formatCount, formatTime } from "../../lib/format";
import { formatError, getScenario, requestEnvelope } from "../../lib/api";
import type { AuditData, UnifiedEvent } from "../../types";

const tabs = [
  { id: "analytics", label: "Analytics" },
  { id: "traffic", label: "Traffic" },
  { id: "security-events", label: "Security events" },
  { id: "sessions", label: "Sessions" },
];

export function AuditPage() {
  const section = currentSection("audit", "analytics");
  const scenario = getScenario();
  const location = useRouterState({ select: (state) => state.location });
  const navigate = useNavigate();
  const triggerRef = useRef<HTMLElement | null>(null);
  const query = useQuery({
    queryKey: ["audit", scenario],
    queryFn: ({ signal }) => requestEnvelope<AuditData>("/api/v1/audit/analytics", signal),
    retry: false,
  });
  const selectedId = new URLSearchParams(location.searchStr).get("event");
  const selected = useMemo(
    () => query.data?.data.events.find((event) => event.id === selectedId),
    [query.data, selectedId],
  );
  const setEvent = useCallback(
    (eventId?: string, trigger?: HTMLTableRowElement) => {
      if (trigger) triggerRef.current = trigger;
      void navigate({
        to: "/audit/$section",
        params: { section },
        search: {
          scenario: scenario === "normal" ? undefined : scenario,
          event: eventId,
        },
        replace: !eventId,
      });
    },
    [navigate, scenario, section],
  );
  const closeEvent = useCallback(() => setEvent(), [setEvent]);
  if (query.isLoading) return <PageSkeleton label="Loading audit data" />;
  if (query.isError || !query.data)
    return (
      <PageFrame>
        <PageHeader
          description="Gateway traffic and AgentGuard security records remain source-distinct."
          eyebrow="Audit / Evidence"
          title="Audit data unavailable"
        />
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      </PageFrame>
    );
  const { data, meta } = query.data;
  return (
    <PageFrame>
      <PageHeader
        actions={
          <Button variant="secondary">
            <Filter size={14} /> Filter
          </Button>
        }
        description="Analyze gateway traffic and runtime security evidence without inventing task-level correlation."
        eyebrow="Audit / Traffic & security"
        title="See every verified signal"
      >
        <WorkspaceTabs area="audit" items={tabs} />
      </PageHeader>
      <PartialBanner meta={meta} />
      {section === "analytics" ? (
        <AnalyticsView data={data} onOpen={(event, trigger) => setEvent(event.id, trigger)} />
      ) : null}
      {section === "traffic" ? (
        <EventsView
          events={data.events.filter((event) => event.source === "agentgateway")}
          onOpen={(event, trigger) => setEvent(event.id, trigger)}
          title="Gateway traffic"
        />
      ) : null}
      {section === "security-events" ? (
        <EventsView
          events={data.events.filter((event) => event.source === "agentguard")}
          onOpen={(event, trigger) => setEvent(event.id, trigger)}
          title="Security events"
        />
      ) : null}
      {section === "sessions" ? <SessionsView data={data} /> : null}
      <DetailDrawer
        eyebrow={selected?.source ?? "Event detail"}
        onClose={closeEvent}
        open={Boolean(selected)}
        returnFocusRef={triggerRef}
        title={selected?.summary ?? "Event not found"}
      >
        {selected ? <EventDetail event={selected} /> : null}
      </DetailDrawer>
    </PageFrame>
  );
}

function AnalyticsView({
  data,
  onOpen,
}: {
  data: AuditData;
  onOpen: (event: UnifiedEvent, trigger: HTMLTableRowElement) => void;
}) {
  if (!data.events.length)
    return (
      <EmptyState
        description="No gateway traffic or AgentGuard security records exist in this time range."
        title="No audit data yet"
      />
    );
  return (
    <>
      <div className="metric-grid">
        {data.metrics.map((metric) => (
          <MetricCard key={metric.id} metric={metric} />
        ))}
      </div>
      <div className="content-grid content-grid--wide">
        <Card className="chart-card">
          <CardHeader
            action={
              <span className="live-caption">
                <Radio size={13} /> 12 buckets
              </span>
            }
            description="Request volume and explicit deny counts; axes are not merged semantically."
            title="Traffic trend"
          />
          <RequestTrendChart data={data.trend} />
        </Card>
        <Card className="chart-card">
          <CardHeader description="P95 gateway request latency." title="Latency trend" />
          <RequestTrendChart data={data.trend} mode="latency" />
        </Card>
      </div>
      <EventsView events={data.events} onOpen={onOpen} title="Unified activity" />
    </>
  );
}

const eventColumns: Column<UnifiedEvent>[] = [
  { key: "time", header: "Timestamp", render: (item) => <time>{formatTime(item.timestamp)}</time> },
  { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  { key: "type", header: "Event type", render: (item) => <StatusBadge status={item.kind} /> },
  {
    key: "severity",
    header: "Severity",
    render: (item) => <SeverityBadge severity={item.severity} />,
  },
  {
    key: "summary",
    header: "Summary",
    className: "table-summary",
    render: (item) => <span>{item.summary}</span>,
  },
  {
    key: "subject",
    header: "Agent / target",
    render: (item) => (
      <code>{item.subject?.agentId ?? item.target?.model ?? item.target?.resource ?? "—"}</code>
    ),
  },
  {
    key: "decision",
    header: "Decision",
    render: (item) =>
      item.decision ? <strong className="decision-text">{item.decision}</strong> : "—",
  },
  {
    key: "correlation",
    header: "Correlation",
    render: (item) =>
      item.correlation?.verified ? (
        <span className="verified-correlation">
          <CheckCircle2 size={12} /> Verified
        </span>
      ) : (
        <span className="no-correlation">
          <CircleSlash size={12} /> None
        </span>
      ),
  },
];

function EventsView({
  events,
  onOpen,
  title,
}: {
  events: UnifiedEvent[];
  onOpen: (event: UnifiedEvent, trigger: HTMLTableRowElement) => void;
  title: string;
}) {
  return events.length ? (
    <Card>
      <CardHeader
        action={
          <span className="fetched-at">
            <ListFilter size={13} /> {events.length} records
          </span>
        }
        description="Select a record for redacted detail. Source IDs remain intact."
        title={title}
      />
      <DataTable columns={eventColumns} data={events} label={title} onRowClick={onOpen} />
    </Card>
  ) : (
    <EmptyState
      description="No records from this source are present in the selected time range."
      title={`No ${title.toLowerCase()}`}
    />
  );
}

function SessionsView({ data }: { data: AuditData }) {
  const columns = [
    {
      key: "session",
      header: "Session",
      render: (item: AuditData["sessions"][number]) => (
        <div className="primary-cell">
          <Activity size={15} />
          <span>
            <strong>{item.id}</strong>
            <small>{item.agentId}</small>
          </span>
        </div>
      ),
    },
    {
      key: "principal",
      header: "Principal",
      render: (item: AuditData["sessions"][number]) => <code>{item.principal}</code>,
    },
    {
      key: "events",
      header: "Events",
      render: (item: AuditData["sessions"][number]) => formatCount(item.events),
    },
    {
      key: "denies",
      header: "Denies",
      render: (item: AuditData["sessions"][number]) => item.denies,
    },
    {
      key: "started",
      header: "Started",
      render: (item: AuditData["sessions"][number]) => item.startedAt,
    },
    {
      key: "status",
      header: "Status",
      render: (item: AuditData["sessions"][number]) => <StatusBadge status={item.status} />,
    },
    {
      key: "source",
      header: "Source",
      render: (item: AuditData["sessions"][number]) => <SourceBadge source={item.source} />,
    },
  ];
  return data.sessions.length ? (
    <Card>
      <CardHeader
        description="AgentGuard sessions only; no task DAG or gateway inference."
        title="Runtime sessions"
      />
      <DataTable columns={columns} data={data.sessions} label="AgentGuard runtime sessions" />
    </Card>
  ) : (
    <EmptyState
      description="AgentGuard has not reported any runtime sessions."
      title="No sessions found"
    />
  );
}

function EventDetail({ event }: { event: UnifiedEvent }) {
  return (
    <div className="event-detail">
      <div className="event-detail__badges">
        <SourceBadge source={event.source} />
        <SeverityBadge severity={event.severity} />
        <StatusBadge status={event.kind} />
      </div>
      <DefinitionList
        items={[
          { label: "Timestamp", value: event.timestamp },
          { label: "Original ID", value: <code>{event.rawRef.id}</code> },
          { label: "Agent", value: event.subject?.agentId ?? "Not provided" },
          { label: "Session", value: event.subject?.sessionId ?? "Not provided" },
          {
            label: "Target",
            value:
              event.target?.tool ?? event.target?.model ?? event.target?.resource ?? "Not provided",
          },
          { label: "Phase", value: event.phase ?? "Not provided" },
          { label: "Action", value: event.action ?? "Not provided" },
          { label: "Decision", value: event.decision ?? "Not provided" },
          {
            label: "Correlation",
            value: event.correlation?.verified ? "Verified shared identifier" : "Not correlated",
          },
        ]}
      />
      <div className="raw-json">
        <header>
          <Braces size={15} />
          <strong>Redacted raw JSON</strong>
        </header>
        <pre>
          <code>{JSON.stringify(event.raw, null, 2)}</code>
        </pre>
      </div>
      <div className="redaction-note">
        <ShieldAlert size={15} /> Prompt, payload, authorization, and tool arguments are redacted in
        this mock detail.
      </div>
    </div>
  );
}
