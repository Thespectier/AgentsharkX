import { AlertTriangle, GitBranch } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import type { TraceDetail } from "../../generated/api-client";
import { formatCount } from "../../lib/format";
import { useI18n } from "../../lib/i18n";
import { cn } from "../../components/ui";
import { TraceFlow } from "../../components/trace-flow";
import { TraceTimeline } from "./trace-timeline";
import { TraceToolbar, type TraceTimelineZoom, type TraceVisualizationView } from "./trace-toolbar";
import {
  buildTraceVisualizationModel,
  getDefaultCollapsedTraceNodes,
  getVisibleTraceRows,
  traceVisualTypes,
  type TraceVisualDiagnosticCode,
  type TraceVisualType,
} from "./trace-visualization-model";

const diagnosticLabels: Record<TraceVisualDiagnosticCode, string> = {
  clock_skew: "clock skew",
  duplicate_span_id: "duplicate Span ID",
  duration_mismatch: "duration mismatch",
  invalid_timestamp: "invalid timestamp",
  missing_parent: "missing parent",
  missing_root: "missing root",
  parent_cycle: "parent cycle",
  root_has_parent: "root has a parent",
};

export function TraceVisualization({
  trace,
  selectedSpanId,
  isLive,
  onSelectSpan,
  defaultView = "flow",
  view,
  onViewChange,
}: {
  trace: TraceDetail;
  selectedSpanId?: string;
  isLive: boolean;
  onSelectSpan: (spanId: string, trigger?: HTMLElement) => void;
  defaultView?: TraceVisualizationView;
  view?: TraceVisualizationView;
  onViewChange?: (view: TraceVisualizationView) => void;
}) {
  const { t } = useI18n();
  const model = useMemo(() => buildTraceVisualizationModel(trace), [trace]);
  const traceIdRef = useRef(trace.summary.traceId);
  const [activeTypes, setActiveTypes] = useState<Set<TraceVisualType>>(
    () => new Set(traceVisualTypes),
  );
  const [errorsOnly, setErrorsOnly] = useState(false);
  const [zoom, setZoom] = useState<TraceTimelineZoom>("fit");
  const [flowNodeLimit, setFlowNodeLimit] = useState(48);
  const [internalView, setInternalView] = useState<TraceVisualizationView>(defaultView);
  const [collapsedIds, setCollapsedIds] = useState(() => getDefaultCollapsedTraceNodes(model));
  const activeView = view ?? internalView;

  useEffect(() => {
    if (traceIdRef.current === trace.summary.traceId) return;
    traceIdRef.current = trace.summary.traceId;
    setActiveTypes(new Set(traceVisualTypes));
    setErrorsOnly(false);
    setZoom("fit");
    setFlowNodeLimit(48);
    setCollapsedIds(getDefaultCollapsedTraceNodes(model));
  }, [model, trace.summary.traceId]);

  const changeView = (nextView: TraceVisualizationView) => {
    if (view === undefined) setInternalView(nextView);
    onViewChange?.(nextView);
  };

  const rowIds = useMemo(
    () =>
      getVisibleTraceRows(model, {
        activeTypes,
        collapsedIds,
        errorsOnly,
        selectedSpanId,
      }),
    [activeTypes, collapsedIds, errorsOnly, model, selectedSpanId],
  );
  const diagnosticCodes = [...new Set(model.diagnostics.map((item) => item.code))];

  return (
    <div className={cn("trace-visualization", isLive && "trace-visualization--live")}>
      <div className="trace-visualization__summary">
        <span>
          <GitBranch aria-hidden="true" size={13} />
          {t("{spans} spans · {links} links", {
            spans: formatCount(trace.totalSpans),
            links: formatCount(trace.totalLinks),
          })}
        </span>
        {activeView === "timeline" && collapsedIds.size ? (
          <span>{t("{count} subtrees collapsed", { count: collapsedIds.size })}</span>
        ) : null}
      </div>
      <TraceToolbar
        activeTypes={activeTypes}
        errorsOnly={errorsOnly}
        flowNodeLimit={flowNodeLimit}
        onFlowNodeLimitChange={setFlowNodeLimit}
        onActiveTypesChange={setActiveTypes}
        onErrorsOnlyChange={setErrorsOnly}
        onViewChange={changeView}
        onZoomChange={setZoom}
        totalCount={model.rows.length}
        visibleCount={rowIds.length}
        view={activeView}
        zoom={zoom}
      />
      {activeView === "flow" ? (
        <TraceFlow
          links={trace.links}
          maxNodes={flowNodeLimit}
          onSelect={(spanId, trigger) => onSelectSpan(spanId, trigger)}
          selectedSpanId={selectedSpanId}
          spans={trace.spans}
        />
      ) : (
        <TraceTimeline
          collapsedIds={collapsedIds}
          model={model}
          onCollapsedIdsChange={setCollapsedIds}
          onSelectSpan={onSelectSpan}
          rowIds={rowIds}
          selectedSpanId={selectedSpanId}
          zoom={zoom}
        />
      )}
      {activeView === "timeline" && diagnosticCodes.length ? (
        <footer className="trace-visualization__diagnostics" role="status">
          <AlertTriangle aria-hidden="true" size={14} />
          <strong>{t("Trace data diagnostics")}</strong>
          <span>{diagnosticCodes.map((code) => t(diagnosticLabels[code])).join(" · ")}</span>
        </footer>
      ) : null}
    </div>
  );
}
