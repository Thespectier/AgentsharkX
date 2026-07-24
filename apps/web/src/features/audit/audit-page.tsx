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
import { useCallback, useMemo, useRef, useState } from "react";

import { RequestTrendChart } from "../../motion/dashboard-motion";
import { PageFrame, useWorkspaceSection } from "../../components/workspace";
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
import { displayTimeZoneLabel, formatCount, formatTimeWithZone } from "../../lib/format";
import { formatError, getScenario, requestOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { mergeLiveEvents, useSharedLiveEvents } from "../../lib/use-live-events";
import type { AuditData, Severity, Source, UnifiedEvent } from "../../types";

type AuditFilters = {
  source: Source | "all";
  severity: Severity | "all";
  query: string;
};

const defaultFilters: AuditFilters = { source: "all", severity: "all", query: "" };

export function AuditPage() {
  const { t } = useI18n();
  const section = useWorkspaceSection("audit", "analytics");
  const scenario = getScenario();
  const location = useRouterState({ select: (state) => state.location });
  const navigate = useNavigate();
  const triggerRef = useRef<HTMLElement | null>(null);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [filters, setFilters] = useState<AuditFilters>(defaultFilters);
  const query = useQuery({
    queryKey: ["audit", scenario],
    queryFn: ({ signal }) => requestOperation("getAuditAnalytics", signal),
    retry: false,
  });
  const live = useSharedLiveEvents();
  const data = useMemo<AuditData | undefined>(() => {
    if (!query.data) return undefined;
    return {
      ...query.data.data,
      events: mergeLiveEvents(live.events, query.data.data.events),
    };
  }, [live.events, query.data]);
  const selectedId = new URLSearchParams(location.searchStr).get("event");
  const selected = useMemo(
    () => data?.events.find((event) => event.id === selectedId),
    [data, selectedId],
  );
  const filteredData = useMemo<AuditData | undefined>(
    () => (data ? { ...data, events: filterAuditEvents(data.events, filters) } : undefined),
    [data, filters],
  );
  const detailQuery = useQuery({
    queryKey: ["audit-event", selected?.source, selected?.id, scenario],
    queryFn: ({ signal }) =>
      requestOperation("getAuditEvent", {
        signal,
        path: { source: selected!.source, eventId: selected!.id },
      }),
    enabled: Boolean(selected),
    retry: false,
  });
  const selectedDetail = detailQuery.data?.data ?? selected;
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
  if (query.isError || !query.data || !data || !filteredData)
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
  const { meta } = query.data;
  return (
    <PageFrame>
      <PageHeader
        actions={
          <Button
            aria-controls="audit-filters"
            aria-expanded={filtersOpen}
            onClick={() => setFiltersOpen((open) => !open)}
            variant="secondary"
          >
            <Filter size={14} /> {t("Filter")}
            {activeFilterCount(filters) ? ` (${activeFilterCount(filters)})` : ""}
          </Button>
        }
        description="Analyze gateway traffic and runtime security evidence without inventing task-level correlation."
        eyebrow="Audit / Traffic & security"
        title="See every verified signal"
      />
      <PartialBanner meta={meta} />
      {filtersOpen ? <AuditFilterPanel filters={filters} onChange={setFilters} /> : null}
      {section === "analytics" ? (
        <AnalyticsView
          data={filteredData}
          onOpen={(event, trigger) => setEvent(event.id, trigger)}
        />
      ) : null}
      {section === "traffic" ? (
        <EventsView
          events={filteredData.events.filter((event) => event.source === "agentgateway")}
          onOpen={(event, trigger) => setEvent(event.id, trigger)}
          title="Gateway traffic"
        />
      ) : null}
      {section === "security-events" ? (
        <EventsView
          events={filteredData.events.filter((event) => event.source === "agentguard")}
          onOpen={(event, trigger) => setEvent(event.id, trigger)}
          title="Security events"
        />
      ) : null}
      {section === "sessions" ? <SessionsView data={filteredData} /> : null}
      <DetailDrawer
        eyebrow={selectedDetail?.source ?? "Event detail"}
        onClose={closeEvent}
        open={Boolean(selected)}
        returnFocusRef={triggerRef}
        title={selectedDetail?.summary ?? "Event not found"}
      >
        {selectedDetail ? <EventDetail event={selectedDetail} /> : null}
      </DetailDrawer>
    </PageFrame>
  );
}

function AuditFilterPanel({
  filters,
  onChange,
}: {
  filters: AuditFilters;
  onChange: (filters: AuditFilters) => void;
}) {
  const { t } = useI18n();
  return (
    <Card className="audit-filters">
      <div id="audit-filters">
        <label>
          <span>{t("Search events")}</span>
          <input
            onChange={(event) => onChange({ ...filters, query: event.target.value })}
            placeholder={t("Summary, agent, model, or resource")}
            value={filters.query}
          />
        </label>
        <label>
          <span>{t("Source")}</span>
          <select
            onChange={(event) =>
              onChange({ ...filters, source: event.target.value as AuditFilters["source"] })
            }
            value={filters.source}
          >
            <option value="all">{t("All sources")}</option>
            <option value="agentgateway">agentgateway</option>
            <option value="agentguard">AgentGuard</option>
          </select>
        </label>
        <label>
          <span>{t("Severity")}</span>
          <select
            onChange={(event) =>
              onChange({ ...filters, severity: event.target.value as AuditFilters["severity"] })
            }
            value={filters.severity}
          >
            <option value="all">{t("All severities")}</option>
            {(["info", "low", "medium", "high", "critical"] as const).map((severity) => (
              <option key={severity} value={severity}>
                {t(severity)}
              </option>
            ))}
          </select>
        </label>
        <Button
          disabled={!activeFilterCount(filters)}
          onClick={() => onChange(defaultFilters)}
          size="sm"
          variant="ghost"
        >
          Reset filters
        </Button>
      </div>
    </Card>
  );
}

export function filterAuditEvents(events: UnifiedEvent[], filters: AuditFilters): UnifiedEvent[] {
  const query = filters.query.trim().toLowerCase();
  return events.filter((event) => {
    if (filters.source !== "all" && event.source !== filters.source) return false;
    if (filters.severity !== "all" && event.severity !== filters.severity) return false;
    if (!query) return true;
    const searchable = [
      event.summary,
      event.subject?.agentId,
      event.subject?.principalId,
      event.subject?.sessionId,
      event.target?.provider,
      event.target?.model,
      event.target?.tool,
      event.target?.resource,
      event.action,
      event.decision,
    ]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();
    return searchable.includes(query);
  });
}

function activeFilterCount(filters: AuditFilters): number {
  return (
    Number(Boolean(filters.query.trim())) +
    Number(filters.source !== "all") +
    Number(filters.severity !== "all")
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
                <Radio size={13} /> 12 × 5m · {displayTimeZoneLabel}
              </span>
            }
            description="Last 60 minutes; verified request volume and explicit denies use independent axes."
            title="Traffic trend"
          />
          <RequestTrendChart data={data.trend} />
        </Card>
        <Card className="chart-card">
          <CardHeader
            description="Nearest-rank P95 from the bounded redacted request-log sample; tooltips show sample size and gaps mean no samples."
            title="Latency trend"
          />
          <RequestTrendChart data={data.trend} mode="latency" />
        </Card>
      </div>
      <EventsView events={data.events} onOpen={onOpen} title="Unified activity" />
    </>
  );
}

const eventColumns: Column<UnifiedEvent>[] = [
  {
    key: "time",
    header: "Timestamp",
    render: (item) => <time>{formatTimeWithZone(item.timestamp)}</time>,
  },
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
  const { t } = useI18n();
  return events.length ? (
    <Card>
      <CardHeader
        action={
          <span className="fetched-at">
            <ListFilter size={13} /> {events.length} {t("records")}
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
  const { t } = useI18n();
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
      key: "last-seen",
      header: "Last seen",
      render: (item: AuditData["sessions"][number]) =>
        item.lastSeen ? formatTimeWithZone(item.lastSeen) : t("Not reported"),
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
        description="AgentGuard sessions only; counts use exact session-ID matches and do not imply a task DAG."
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
  const { t } = useI18n();
  return (
    <div className="event-detail">
      <div className="event-detail__badges">
        <SourceBadge source={event.source} />
        <SeverityBadge severity={event.severity} />
        <StatusBadge status={event.kind} />
      </div>
      <DefinitionList
        items={[
          { label: "Timestamp", value: formatTimeWithZone(event.timestamp) },
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
          <strong>{t("Redacted raw JSON")}</strong>
        </header>
        <pre>
          <code>{JSON.stringify(event.raw ?? { redacted: true }, null, 2)}</code>
        </pre>
      </div>
      <div className="redaction-note">
        <ShieldAlert size={15} /> Prompt, payload, authorization, and tool arguments are redacted in
        this detail.
      </div>
    </div>
  );
}
