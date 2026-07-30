import { useQuery } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import {
  AlertTriangle,
  ArrowLeft,
  Braces,
  ChevronLeft,
  ChevronRight,
  CircleDotDashed,
  Filter,
  GitBranch,
  ListTree,
  Radio,
  RefreshCw,
  Search,
} from "lucide-react";
import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  Button,
  Card,
  CardHeader,
  DataTable,
  DefinitionList,
  DetailDrawer,
  EmptyState,
  ErrorState,
  InlineLoading,
  PageHeader,
  PageSkeleton,
  StatusBadge,
  type Column,
} from "../../components/ui";
import { PageFrame } from "../../components/workspace";
import type {
  TraceDetail,
  TraceListEnvelope,
  TraceSpan,
  TraceSpanDetail,
  TraceSummary,
} from "../../generated/api-client";
import { ApiError, formatError, getScenario, requestOperation } from "../../lib/api";
import { formatCount, formatDateTimeWithZone } from "../../lib/format";
import { useI18n } from "../../lib/i18n";
import { TraceFlow } from "./trace-flow";

const pageSize = 25;
const initialSpanRows = 100;
type TraceListData = TraceListEnvelope["data"];
type Translator = ReturnType<typeof useI18n>["t"];

type TraceFilters = {
  cursor: string;
  cursorHistory: string[];
  status: string;
  completeness: string;
  agentId: string;
  hasError: string;
  hasA2A: string;
  startedAfter: string;
  startedBefore: string;
  query: string;
};

const emptyFilters: TraceFilters = {
  cursor: "",
  cursorHistory: [],
  status: "",
  completeness: "",
  agentId: "",
  hasError: "",
  hasA2A: "",
  startedAfter: "",
  startedBefore: "",
  query: "",
};

export function TraceListPage() {
  const { t } = useI18n();
  const location = useRouterState({ select: (state) => state.location });
  const navigate = useNavigate();
  const scenario = getScenario();
  const filters = useMemo(() => traceFiltersFromSearch(location.searchStr), [location.searchStr]);
  const [draft, setDraft] = useState(filters);
  const filterKey = stableFilterKey(filters);
  const query = useQuery({
    queryKey: ["audit-traces", scenario, filterKey],
    queryFn: ({ signal }) =>
      requestOperation("listAuditTraces", {
        signal,
        query: traceRequestQuery(filters),
      }),
    retry: false,
    refetchInterval: filters.cursor || scenario === "loading" ? false : 5_000,
  });
  const [displayed, setDisplayed] = useState<TraceListData>();
  const [pending, setPending] = useState<TraceListData>();
  const visibleKey = useRef(filterKey);

  useEffect(() => setDraft(filters), [filterKey]);
  useEffect(() => {
    if (visibleKey.current === filterKey) return;
    visibleKey.current = filterKey;
    setDisplayed(undefined);
    setPending(undefined);
  }, [filterKey]);
  useEffect(() => {
    if (!query.data) return;
    if (!displayed) {
      setDisplayed(query.data.data);
      setPending(undefined);
      return;
    }
    if (pending === query.data.data) return;
    const reconciled = reconcileTracePage(displayed, query.data.data, !filters.cursor);
    setDisplayed(reconciled.displayed);
    setPending(reconciled.pending);
  }, [displayed, filterKey, filters.cursor, pending, query.data]);

  const updateFilters = useCallback(
    (next: TraceFilters) => {
      const search = traceSearchParams(next, scenario);
      void navigate({ href: `/audit/traces${search ? `?${search}` : ""}` });
    },
    [navigate, scenario],
  );
  const submit = (event: FormEvent) => {
    event.preventDefault();
    updateFilters({ ...draft, cursor: "", cursorHistory: [] });
  };

  if (query.isLoading && !displayed) return <PageSkeleton label="Loading traces" />;
  if (query.isError || (!query.data && !displayed)) {
    return <TraceQueryError error={query.error} onRetry={() => void query.refetch()} />;
  }
  const data = displayed ?? query.data!.data;
  const newCount = pending
    ? pending.items.filter((trace) => !data.items.some((item) => item.traceId === trace.traceId))
        .length
    : 0;
  return (
    <PageFrame>
      <PageHeader
        actions={
          <Button onClick={() => void query.refetch()} variant="secondary">
            <RefreshCw aria-hidden="true" size={14} /> Refresh
          </Button>
        }
        description="Find one explicit Agent task execution, then inspect its deterministic spans and links."
        eyebrow="Audit / Traces"
        title="Trace every agent task"
      />
      <TraceFilterPanel
        draft={draft}
        onChange={setDraft}
        onReset={() => updateFilters(emptyFilters)}
        onSubmit={submit}
      />
      {newCount > 0 ? (
        <button
          className="trace-new-notice"
          onClick={() => {
            setDisplayed(pending);
            setPending(undefined);
          }}
          type="button"
        >
          <Radio aria-hidden="true" size={15} />
          <span>
            <strong>{t("{count} new traces available", { count: newCount })}</strong>
          </span>
          <span>{t("Show updates")}</span>
        </button>
      ) : null}
      {data.items.length ? (
        <Card className="trace-list-card">
          <CardHeader
            action={
              <span className="fetched-at">
                {t("{count} total", { count: formatCount(data.total) })}
              </span>
            }
            description="Stable ingest ordering; complete content is available only from an opened Span."
            title="Task traces"
          />
          <DataTable
            columns={traceColumns}
            data={data.items.map((trace) => ({ ...trace, id: trace.traceId }))}
            label="Task traces"
            onRowClick={(trace) => {
              void navigate({ href: `/audit/traces/${trace.traceId}${location.searchStr}` });
            }}
          />
          <div className="trace-pagination" aria-label="Trace pagination">
            <span>
              {t("Showing {shown} of {total} matching traces", {
                shown: formatCount(data.items.length),
                total: formatCount(data.total),
              })}
            </span>
            <div>
              <Button
                disabled={!filters.cursorHistory.length}
                onClick={() => {
                  const history = [...filters.cursorHistory];
                  const cursor = history.pop() ?? "";
                  updateFilters({ ...filters, cursor, cursorHistory: history });
                }}
                size="sm"
                variant="ghost"
              >
                <ChevronLeft aria-hidden="true" size={14} /> Previous
              </Button>
              <Button
                disabled={!data.nextCursor}
                onClick={() =>
                  updateFilters({
                    ...filters,
                    cursor: data.nextCursor ?? "",
                    cursorHistory: [...filters.cursorHistory, filters.cursor],
                  })
                }
                size="sm"
                variant="ghost"
              >
                Next <ChevronRight aria-hidden="true" size={14} />
              </Button>
            </div>
          </div>
        </Card>
      ) : (
        <EmptyState
          action={
            activeTraceFilterCount(filters) ? (
              <Button onClick={() => updateFilters(emptyFilters)}>Clear filters</Button>
            ) : undefined
          }
          description={
            activeTraceFilterCount(filters)
              ? "No task Trace matches the current filters."
              : "Send OTLP traces from an AgentsharkX SDK runtime to begin task-level analysis."
          }
          title="No traces found"
        />
      )}
    </PageFrame>
  );
}

function TraceFilterPanel({
  draft,
  onChange,
  onReset,
  onSubmit,
}: {
  draft: TraceFilters;
  onChange: (filters: TraceFilters) => void;
  onReset: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const { t } = useI18n();
  return (
    <Card className="trace-filters">
      <form onSubmit={onSubmit}>
        <label className="trace-filters__search">
          <span>{t("Task, session, or Trace ID")}</span>
          <div>
            <Search aria-hidden="true" size={14} />
            <input
              aria-label={t("Search traces")}
              onChange={(event) => onChange({ ...draft, query: event.target.value })}
              placeholder={t("Search task, session, or Trace ID")}
              value={draft.query}
            />
          </div>
        </label>
        <label>
          <span>{t("Root agent")}</span>
          <input
            aria-label={t("Filter by root agent")}
            onChange={(event) => onChange({ ...draft, agentId: event.target.value })}
            placeholder={t("Agent ID")}
            value={draft.agentId}
          />
        </label>
        <label>
          <span>{t("Status")}</span>
          <select
            aria-label={t("Filter by trace status")}
            onChange={(event) => onChange({ ...draft, status: event.target.value })}
            value={draft.status}
          >
            <option value="">{t("All statuses")}</option>
            <option value="running">{t("Running")}</option>
            <option value="succeeded">{t("Succeeded")}</option>
            <option value="failed">{t("Failed")}</option>
            <option value="unknown">{t("Unknown")}</option>
          </select>
        </label>
        <label>
          <span>{t("Completeness")}</span>
          <select
            aria-label={t("Filter by completeness")}
            onChange={(event) => onChange({ ...draft, completeness: event.target.value })}
            value={draft.completeness}
          >
            <option value="">{t("All coverage")}</option>
            <option value="verified">{t("Verified")}</option>
            <option value="partial">{t("Partial")}</option>
          </select>
        </label>
        <label>
          <span>{t("Errors")}</span>
          <select
            aria-label={t("Filter by errors")}
            onChange={(event) => onChange({ ...draft, hasError: event.target.value })}
            value={draft.hasError}
          >
            <option value="">{t("Any")}</option>
            <option value="true">{t("Has errors")}</option>
            <option value="false">{t("No errors")}</option>
          </select>
        </label>
        <label>
          <span>A2A</span>
          <select
            aria-label={t("Filter by A2A")}
            onChange={(event) => onChange({ ...draft, hasA2A: event.target.value })}
            value={draft.hasA2A}
          >
            <option value="">{t("Any")}</option>
            <option value="true">{t("Observed")}</option>
            <option value="false">{t("Not observed")}</option>
          </select>
        </label>
        <label>
          <span>{t("Started after (UTC+8)")}</span>
          <input
            aria-label={t("Started after")}
            onChange={(event) => onChange({ ...draft, startedAfter: event.target.value })}
            type="datetime-local"
            value={draft.startedAfter}
          />
        </label>
        <label>
          <span>{t("Started before (UTC+8)")}</span>
          <input
            aria-label={t("Started before")}
            onChange={(event) => onChange({ ...draft, startedBefore: event.target.value })}
            type="datetime-local"
            value={draft.startedBefore}
          />
        </label>
        <div className="trace-filters__actions">
          <Button
            disabled={!activeTraceFilterCount(draft)}
            onClick={onReset}
            type="button"
            variant="ghost"
          >
            Reset
          </Button>
          <Button type="submit" variant="primary">
            <Filter aria-hidden="true" size={14} /> Apply
          </Button>
        </div>
      </form>
    </Card>
  );
}

export function TraceDetailPage() {
  const { t } = useI18n();
  const location = useRouterState({ select: (state) => state.location });
  const navigate = useNavigate();
  const scenario = getScenario();
  const traceId = location.pathname.split("/").filter(Boolean).at(-1) ?? "";
  const selectedSpanId = new URLSearchParams(location.searchStr).get("span") ?? undefined;
  const triggerRef = useRef<HTMLElement | null>(null);
  const [spanRowLimit, setSpanRowLimit] = useState(initialSpanRows);
  const [flowNodeLimit, setFlowNodeLimit] = useState(48);
  const listSearch = useMemo(() => {
    const search = new URLSearchParams(location.searchStr);
    search.delete("span");
    return search.toString();
  }, [location.searchStr]);
  const detailQuery = useQuery({
    queryKey: ["audit-trace", traceId, scenario],
    queryFn: ({ signal }) => requestOperation("getAuditTrace", { signal, path: { traceId } }),
    retry: false,
    refetchInterval: (query) => {
      const detail = query.state.data?.data;
      return detail?.summary.status === "running" || detail?.summary.completeness === "partial"
        ? 2_500
        : false;
    },
  });
  const spanQuery = useQuery({
    queryKey: ["audit-trace-span", traceId, selectedSpanId, scenario],
    queryFn: ({ signal }) =>
      requestOperation("getAuditTraceSpan", {
        signal,
        path: { traceId, spanId: selectedSpanId! },
      }),
    enabled: Boolean(selectedSpanId),
    retry: false,
  });
  const setSelectedSpan = useCallback(
    (spanId?: string, trigger?: HTMLElement) => {
      if (trigger) triggerRef.current = trigger;
      const search = new URLSearchParams(location.searchStr);
      if (spanId) search.set("span", spanId);
      else search.delete("span");
      void navigate({
        href: `${location.pathname}${search.size ? `?${search}` : ""}`,
        replace: !spanId,
      });
    },
    [location.pathname, location.searchStr, navigate],
  );

  if (detailQuery.isLoading) return <PageSkeleton label="Loading trace detail" />;
  if (detailQuery.isError || !detailQuery.data) {
    return (
      <TraceQueryError
        detail
        error={detailQuery.error}
        onRetry={() => void detailQuery.refetch()}
      />
    );
  }
  const detail = detailQuery.data.data;
  const selectedSummary = detail.spans.find((span) => span.spanId === selectedSpanId);
  return (
    <PageFrame>
      <PageHeader
        actions={
          <Button onClick={() => void detailQuery.refetch()} variant="secondary">
            <RefreshCw aria-hidden="true" size={14} /> Refresh
          </Button>
        }
        description="Explicit parent relationships and Span Links only; time proximity never creates an edge."
        eyebrow="Audit / Traces / Detail"
        title={detail.summary.taskId || `Trace ${shortID(detail.summary.traceId)}`}
      >
        <button
          className="trace-back-link"
          onClick={() =>
            void navigate({ href: `/audit/traces${listSearch ? `?${listSearch}` : ""}` })
          }
          type="button"
        >
          <ArrowLeft aria-hidden="true" size={14} /> {t("Back to traces")}
        </button>
      </PageHeader>
      <TraceStateBanner detail={detail} />
      <TraceIdentity detail={detail} />
      <TraceMetrics summary={detail.summary} />
      <Card className="trace-flow-card">
        <CardHeader
          action={
            <div className="trace-flow-controls">
              <span className="fetched-at">
                <GitBranch size={13} />
                {t("{spans} spans · {links} links", {
                  spans: formatCount(detail.totalSpans),
                  links: formatCount(detail.totalLinks),
                })}
              </span>
              <label>
                <span>{t("Nodes")}</span>
                <select
                  aria-label={t("TraceFlow node limit")}
                  onChange={(event) => setFlowNodeLimit(Number(event.target.value))}
                  value={flowNodeLimit}
                >
                  <option value="24">24</option>
                  <option value="48">48</option>
                  <option value="72">72</option>
                </select>
              </label>
            </div>
          }
          description="Solid lines are verified parent_span_id edges. Dashed lines are explicit Span Links."
          title="TraceFlow"
        />
        <TraceFlow
          links={detail.links}
          maxNodes={flowNodeLimit}
          onSelect={(spanId, trigger) => setSelectedSpan(spanId, trigger)}
          selectedSpanId={selectedSpanId}
          spans={detail.spans}
        />
      </Card>
      <TraceSpanTable
        detail={detail}
        limit={spanRowLimit}
        onMore={() => setSpanRowLimit((value) => value + initialSpanRows)}
        onSelect={(span, trigger) => setSelectedSpan(span.spanId, trigger)}
      />
      <DetailDrawer
        eyebrow={selectedSummary?.openInferenceKind || "Span detail"}
        onClose={() => setSelectedSpan()}
        open={Boolean(selectedSpanId)}
        returnFocusRef={triggerRef}
        title={selectedSummary?.name ?? "Span detail"}
      >
        {spanQuery.isLoading ? (
          <div className="trace-span-loading">
            <InlineLoading label="Loading complete Span detail" />
          </div>
        ) : null}
        {spanQuery.isError ? (
          <TraceSpanError error={spanQuery.error} onRetry={() => void spanQuery.refetch()} />
        ) : null}
        {spanQuery.data ? <TraceSpanDetailView detail={spanQuery.data.data} /> : null}
      </DetailDrawer>
    </PageFrame>
  );
}

function TraceStateBanner({ detail }: { detail: TraceDetail }) {
  const { t } = useI18n();
  if (
    detail.summary.status !== "running" &&
    detail.summary.completeness !== "partial" &&
    !detail.spansTruncated &&
    !detail.linksTruncated
  )
    return null;
  const running = detail.summary.status === "running";
  return (
    <div
      className={`trace-state-banner ${running ? "trace-state-banner--running" : ""}`}
      role="status"
    >
      <CircleDotDashed aria-hidden="true" size={17} />
      <div>
        <strong>{t(running ? "Trace is still running" : "Partial Trace")}</strong>
        <span>
          {t(
            running
              ? "New spans will appear without clearing the selected node."
              : "Available spans remain analyzable; missing roots or identifiers are not inferred.",
          )}
          {detail.spansTruncated || detail.linksTruncated
            ? ` ${t("The API bounded this graph response.")}`
            : ""}
        </span>
      </div>
    </div>
  );
}

function TraceIdentity({ detail }: { detail: TraceDetail }) {
  const { t } = useI18n();
  const summary = detail.summary;
  return (
    <Card className="trace-identity">
      <div>
        <span>
          <small>{t("Status")}</small>
          <StatusBadge status={summary.status} />
        </span>
        <span>
          <small>{t("Completeness")}</small>
          <StatusBadge status={summary.completeness} />
        </span>
        <span>
          <small>{t("Task")}</small>
          <code>{summary.taskId || t("Not reported")}</code>
        </span>
        <span>
          <small>{t("Session")}</small>
          <code>{summary.sessionId || t("Not reported")}</code>
        </span>
        <span>
          <small>{t("Root agent")}</small>
          <strong>{summary.rootAgentId || t("Not reported")}</strong>
        </span>
        <span>
          <small>{t("Trace ID")}</small>
          <code>{summary.traceId}</code>
        </span>
      </div>
      <div className="trace-coverage">
        <strong>{t("Coverage")}</strong>
        <span>{coverageSummary(detail, t)}</span>
      </div>
    </Card>
  );
}

function TraceMetrics({ summary }: { summary: TraceSummary }) {
  const { t } = useI18n();
  const metrics = [
    ["LLM calls", formatCount(summary.llmCalls)],
    ["MCP calls", formatCount(summary.mcpCalls)],
    ["Local tools", formatCount(summary.localToolCalls)],
    [
      "A2A calls",
      summary.a2aCalls ? formatCount(summary.a2aCalls) : t("No A2A interaction observed"),
    ],
    ["Tokens", formatCount(summary.totalTokens)],
    ["Duration", t(formatTraceDuration(summary.durationMs, summary.status))],
    ["Errors", formatCount(summary.errorCount)],
    ["Risk", summary.riskLevel || t("Not reported")],
  ];
  return (
    <div className="trace-metrics">
      {metrics.map(([label, value]) => (
        <article key={label}>
          <small>{t(label)}</small>
          <strong>{value}</strong>
        </article>
      ))}
    </div>
  );
}

function TraceSpanTable({
  detail,
  limit,
  onMore,
  onSelect,
}: {
  detail: TraceDetail;
  limit: number;
  onMore: () => void;
  onSelect: (span: TraceSpan, trigger: HTMLTableRowElement) => void;
}) {
  const { t } = useI18n();
  const visible = detail.spans.slice(0, limit).map((span) => ({ ...span, id: span.spanId }));
  return (
    <Card className="trace-span-card">
      <CardHeader
        action={
          <span className="fetched-at">
            <ListTree size={13} />
            {t("{count} visible", { count: formatCount(detail.spans.length) })}
          </span>
        }
        description="Stable start-time order. Select a row to retrieve attributes, events, and retained content on demand."
        title="Spans & interactions"
      />
      <DataTable
        columns={spanColumns}
        data={visible}
        label="Trace spans and interactions"
        onRowClick={onSelect}
      />
      {visible.length < detail.spans.length ? (
        <div className="trace-table-more">
          <Button onClick={onMore} variant="ghost">
            {t("Show {count} more spans", {
              count: Math.min(initialSpanRows, detail.spans.length - visible.length),
            })}
          </Button>
        </div>
      ) : null}
    </Card>
  );
}

function TraceSpanDetailView({ detail }: { detail: TraceSpanDetail }) {
  const { t } = useI18n();
  const span = detail.span;
  return (
    <div className="trace-span-detail">
      <div className="event-detail__badges">
        <StatusBadge status={span.statusCode} />
        <StatusBadge status={span.contentState.replaceAll("_", " ")} />
      </div>
      <DefinitionList
        items={[
          { label: "Span ID", value: <code>{span.spanId}</code> },
          {
            label: "Parent Span",
            value: span.parentSpanId ? <code>{span.parentSpanId}</code> : t("Root or unparented"),
          },
          {
            label: "Started",
            value: <time dateTime={span.startedAt}>{formatDateTimeWithZone(span.startedAt)}</time>,
          },
          {
            label: "Ended",
            value: span.endedAt ? (
              <time dateTime={span.endedAt}>{formatDateTimeWithZone(span.endedAt)}</time>
            ) : (
              t("Still running")
            ),
          },
          {
            label: "Duration",
            value: formatTraceDuration(span.durationMs, span.endedAt ? span.statusCode : "running"),
          },
          { label: "Agent", value: span.agentId || t("Not reported") },
          { label: "Operation kind", value: span.openInferenceKind || t("Not reported") },
          {
            label: "Provider / model",
            value: [span.provider, span.model].filter(Boolean).join(" / ") || t("Not reported"),
          },
          {
            label: "Tool",
            value: [span.mcpServer, span.toolName].filter(Boolean).join(" / ") || t("Not reported"),
          },
          { label: "Peer agent", value: span.peerAgentId || t("No A2A interaction observed") },
          { label: "Status message", value: detail.statusMessage || t("Not reported") },
        ]}
      />
      <ContentStateNotice state={span.contentState} hasPayload={detail.payloads.length > 0} />
      {detail.payloads.map((payload, index) => (
        <RawTraceJSON
          key={`${payload.kind}:${index}`}
          title={`${payload.kind} · ${t(payload.redactionState)}`}
          value={payload.payloadJson ?? payload.payloadBytes ?? t("No retained body")}
        />
      ))}
      <RawTraceJSON title="Attributes" value={detail.attributes} />
      <RawTraceJSON title="Resource" value={detail.resource} />
      <RawTraceJSON title="Span events" value={detail.events} />
    </div>
  );
}

function ContentStateNotice({ state, hasPayload }: { state: string; hasPayload: boolean }) {
  const { t } = useI18n();
  const messages: Record<string, string> = {
    captured: hasPayload
      ? "Captured content is available below."
      : "Content is marked captured, but no retained payload is available.",
    redacted: "Content was redacted at collection.",
    truncated: "Captured content is truncated.",
    not_collected: "Payload content was not collected.",
    expired: "Payload retention has expired.",
  };
  return (
    <div className={`trace-content-state trace-content-state--${state}`} role="status">
      <AlertTriangle aria-hidden="true" size={16} />
      <span>
        <strong>{t("Content state")}</strong>
        {t(messages[state] ?? "Content state: {state}", { state })}
      </span>
    </div>
  );
}

function RawTraceJSON({ title, value }: { title: string; value: unknown }) {
  const { t } = useI18n();
  return (
    <section className="raw-json trace-raw-json">
      <header>
        <Braces aria-hidden="true" size={15} />
        <strong>{t(title)}</strong>
      </header>
      <pre>
        <code>{typeof value === "string" ? value : JSON.stringify(value, null, 2)}</code>
      </pre>
    </section>
  );
}

function TraceQueryError({
  error,
  onRetry,
  detail = false,
}: {
  error: unknown;
  onRetry: () => void;
  detail?: boolean;
}) {
  const status = error instanceof ApiError ? error.status : 0;
  const title =
    status === 404
      ? "Trace not found"
      : status === 403
        ? "Trace access denied"
        : status === 503
          ? "Trace database unavailable"
          : detail
            ? "Trace detail unavailable"
            : "Traces unavailable";
  return (
    <PageFrame>
      <PageHeader
        description="The failure is isolated to Trace storage and does not affect upstream Audit evidence."
        eyebrow="Audit / Traces"
        title={title}
      />
      <ErrorState
        description={formatError(error)}
        onRetry={status === 403 || status === 404 ? undefined : onRetry}
        title={title}
      />
    </PageFrame>
  );
}

function TraceSpanError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  const status = error instanceof ApiError ? error.status : 0;
  return (
    <ErrorState
      description={formatError(error)}
      onRetry={status === 403 || status === 404 ? undefined : onRetry}
      title={
        status === 404
          ? "Span detail not found"
          : status === 403
            ? "Span content access denied"
            : "Span detail unavailable"
      }
    />
  );
}

const traceColumns: Column<TraceSummary & { id: string }>[] = [
  {
    key: "task",
    header: "Task / Trace",
    render: (trace) => (
      <div className="primary-cell trace-primary-cell">
        <ListTree size={15} />
        <span>
          <strong>{trace.taskId || shortID(trace.traceId)}</strong>
          <small>{trace.traceId}</small>
          <span className="trace-row-details">
            <span>LLM {formatCount(trace.llmCalls)}</span>
            <span>MCP {formatCount(trace.mcpCalls)}</span>
            <span>Local {formatCount(trace.localToolCalls)}</span>
            <span>Tokens {formatCount(trace.totalTokens)}</span>
            <span>Risk {trace.riskLevel || "—"}</span>
          </span>
        </span>
      </div>
    ),
  },
  { key: "agent", header: "Root agent", render: (trace) => trace.rootAgentId || "Not reported" },
  { key: "status", header: "Status", render: (trace) => <StatusBadge status={trace.status} /> },
  {
    key: "completeness",
    header: "Complete",
    render: (trace) => <StatusBadge status={trace.completeness} />,
  },
  {
    key: "llm",
    header: "LLM",
    className: "trace-column--secondary",
    render: (trace) => formatCount(trace.llmCalls),
  },
  {
    key: "mcp",
    header: "MCP",
    className: "trace-column--secondary",
    render: (trace) => formatCount(trace.mcpCalls),
  },
  {
    key: "tools",
    header: "Local",
    className: "trace-column--secondary",
    render: (trace) => formatCount(trace.localToolCalls),
  },
  {
    key: "a2a",
    header: "A2A",
    render: (trace) =>
      trace.a2aCalls ? (
        <span className="trace-peer-observed">{formatCount(trace.a2aCalls)} · observed</span>
      ) : (
        "Not observed"
      ),
  },
  {
    key: "tokens",
    header: "Tokens",
    className: "trace-column--secondary",
    render: (trace) => formatCount(trace.totalTokens),
  },
  {
    key: "duration",
    header: "Duration",
    render: (trace) => formatTraceDuration(trace.durationMs, trace.status),
  },
  {
    key: "errors",
    header: "Errors",
    render: (trace) => (
      <span className={trace.errorCount ? "trace-error-count" : undefined}>
        {formatCount(trace.errorCount)}
      </span>
    ),
  },
  {
    key: "risk",
    header: "Risk",
    className: "trace-column--secondary",
    render: (trace) => trace.riskLevel || "—",
  },
  {
    key: "updated",
    header: "Last updated",
    render: (trace) => (
      <time dateTime={trace.updatedAt}>{formatDateTimeWithZone(trace.updatedAt)}</time>
    ),
  },
];

const spanColumns: Column<TraceSpan & { id: string }>[] = [
  {
    key: "span",
    header: "Span / interaction",
    render: (span) => (
      <div className="primary-cell trace-primary-cell">
        <GitBranch size={15} />
        <span>
          <strong>{span.name}</strong>
          <small>{span.spanId}</small>
        </span>
      </div>
    ),
  },
  {
    key: "kind",
    header: "Kind",
    render: (span) => span.openInferenceKind || span.toolKind || "Agent",
  },
  { key: "agent", header: "Agent", render: (span) => span.peerAgentId || span.agentId || "—" },
  {
    key: "target",
    header: "Target",
    render: (span) => span.model || span.toolName || span.mcpServer || "—",
  },
  {
    key: "started",
    header: "Started",
    render: (span) => (
      <time dateTime={span.startedAt}>{formatDateTimeWithZone(span.startedAt)}</time>
    ),
  },
  {
    key: "duration",
    header: "Duration",
    render: (span) =>
      formatTraceDuration(span.durationMs, span.endedAt ? span.statusCode : "running"),
  },
  {
    key: "status",
    header: "Status",
    render: (span) => <StatusBadge status={span.endedAt ? span.statusCode : "running"} />,
  },
  {
    key: "content",
    header: "Content",
    render: (span) => <StatusBadge status={span.contentState.replaceAll("_", " ")} />,
  },
];

function traceFiltersFromSearch(searchString: string): TraceFilters {
  const search = new URLSearchParams(searchString);
  let cursorHistory: string[] = [];
  try {
    const decoded = JSON.parse(search.get("trace_history") ?? "[]");
    if (Array.isArray(decoded))
      cursorHistory = decoded.filter((item): item is string => typeof item === "string");
  } catch {
    cursorHistory = [];
  }
  return {
    cursor: search.get("cursor") ?? "",
    cursorHistory,
    status: search.get("status") ?? "",
    completeness: search.get("completeness") ?? "",
    agentId: search.get("agent_id") ?? "",
    hasError: search.get("has_error") ?? "",
    hasA2A: search.get("has_a2a") ?? "",
    startedAfter: search.get("started_after") ?? "",
    startedBefore: search.get("started_before") ?? "",
    query: search.get("query") ?? "",
  };
}

function traceRequestQuery(filters: TraceFilters): Record<string, string | number | undefined> {
  return {
    cursor: filters.cursor || undefined,
    limit: pageSize,
    status: filters.status || undefined,
    completeness: filters.completeness || undefined,
    agent_id: filters.agentId.trim() || undefined,
    has_error: filters.hasError || undefined,
    has_a2a: filters.hasA2A || undefined,
    started_after: dateTimeQuery(filters.startedAfter),
    started_before: dateTimeQuery(filters.startedBefore),
    query: filters.query.trim() || undefined,
  };
}

function traceSearchParams(filters: TraceFilters, scenario: string): string {
  const search = new URLSearchParams();
  if (scenario !== "normal") search.set("scenario", scenario);
  const values: Record<string, string> = {
    cursor: filters.cursor,
    status: filters.status,
    completeness: filters.completeness,
    agent_id: filters.agentId.trim(),
    has_error: filters.hasError,
    has_a2a: filters.hasA2A,
    started_after: filters.startedAfter,
    started_before: filters.startedBefore,
    query: filters.query.trim(),
  };
  for (const [key, value] of Object.entries(values)) if (value) search.set(key, value);
  if (filters.cursorHistory.length)
    search.set("trace_history", JSON.stringify(filters.cursorHistory));
  return search.toString();
}

function stableFilterKey(filters: TraceFilters): string {
  return JSON.stringify(traceRequestQuery(filters));
}
function reconcileTracePage(
  displayed: TraceListData,
  incoming: TraceListData,
  holdNewInserts: boolean,
): { displayed: TraceListData; pending?: TraceListData; newCount: number } {
  if (displayed === incoming) return { displayed, newCount: 0 };
  const currentIDs = new Set(displayed.items.map((trace) => trace.traceId));
  const newCount = incoming.items.filter((trace) => !currentIDs.has(trace.traceId)).length;
  if (!holdNewInserts || newCount === 0) return { displayed: incoming, newCount };
  const updates = new Map(incoming.items.map((trace) => [trace.traceId, trace]));
  return {
    displayed: {
      ...displayed,
      items: displayed.items.map((trace) => updates.get(trace.traceId) ?? trace),
    },
    pending: incoming,
    newCount,
  };
}
function activeTraceFilterCount(filters: TraceFilters): number {
  return [
    filters.status,
    filters.completeness,
    filters.agentId.trim(),
    filters.hasError,
    filters.hasA2A,
    filters.startedAfter,
    filters.startedBefore,
    filters.query.trim(),
  ].filter(Boolean).length;
}
function dateTimeQuery(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(`${value.length === 16 ? `${value}:00` : value}+08:00`);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}
function formatTraceDuration(value: number | null | undefined, status?: string): string {
  if (value === null || value === undefined)
    return status === "running" || status === "unset" ? "Running" : "Not verified";
  if (value < 1_000) return `${formatCount(value)} ms`;
  if (value < 60_000) return `${(value / 1_000).toFixed(2)} s`;
  return `${(value / 60_000).toFixed(1)} min`;
}
function shortID(value: string): string {
  return value.length > 16 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}
function coverageSummary(detail: TraceDetail, t: Translator): string {
  const coverage = detail.coverage;
  const groups: Array<[string, string[]]> = [
    ["agents", coverage.agentIds],
    ["peer agents", coverage.peerAgentIds],
    ["providers", coverage.providers],
    ["models", coverage.models],
    ["MCP servers", coverage.mcpServers],
    ["span kinds", coverage.spanKinds],
    ["instrumentation scopes", coverage.instrumentationScopes],
    ["content states", coverage.contentStates],
  ];
  const visible = groups
    .filter(([, values]) => values.length)
    .map(([label, values]) => `${values.length} ${t(label)}`);
  return [
    coverage.source,
    ...(visible.length ? visible : [t("No coverage metadata reported")]),
  ].join(" · ");
}

export const tracePageTestHelpers = {
  traceFiltersFromSearch,
  traceRequestQuery,
  buildSearch: traceSearchParams,
  formatTraceDuration,
  reconcileTracePage,
};
