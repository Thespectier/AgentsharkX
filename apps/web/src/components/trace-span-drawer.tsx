import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Braces } from "lucide-react";
import type { RefObject } from "react";

import type { TraceSpan, TraceSpanDetail } from "../generated/api-client";
import { ApiError, formatError, getScenario, requestOperation } from "../lib/api";
import { formatDateTimeWithZone, formatTraceDuration } from "../lib/format";
import { useI18n } from "../lib/i18n";
import { DefinitionList, DetailDrawer, ErrorState, InlineLoading, StatusBadge } from "./ui";

export function TraceSpanDrawer({
  traceId,
  spanId,
  span,
  onClose,
  returnFocusRef,
}: {
  traceId: string;
  spanId?: string;
  span?: TraceSpan;
  onClose: () => void;
  returnFocusRef?: RefObject<HTMLElement | null>;
}) {
  const scenario = getScenario();
  const query = useQuery({
    queryKey: ["audit-trace-span", traceId, spanId, scenario],
    queryFn: ({ signal }) =>
      requestOperation("getAuditTraceSpan", {
        signal,
        path: { traceId, spanId: spanId! },
      }),
    enabled: Boolean(spanId),
    retry: false,
  });

  return (
    <DetailDrawer
      eyebrow={span?.openInferenceKind || "Span detail"}
      onClose={onClose}
      open={Boolean(spanId)}
      returnFocusRef={returnFocusRef}
      title={span?.name ?? "Span detail"}
    >
      {query.isLoading ? (
        <div className="trace-span-loading">
          <InlineLoading label="Loading complete Span detail" />
        </div>
      ) : null}
      {query.isError ? (
        <TraceSpanError error={query.error} onRetry={() => void query.refetch()} />
      ) : null}
      {query.data ? <TraceSpanDetailView detail={query.data.data} /> : null}
    </DetailDrawer>
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
