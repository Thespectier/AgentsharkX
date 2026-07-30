import type { TraceDetail, TraceLink, TraceSpan } from "../../generated/api-client";

export const traceVisualTypes = [
  "agent",
  "llm",
  "mcp",
  "tool",
  "peer",
  "retriever",
  "unknown",
] as const;

export type TraceVisualType = (typeof traceVisualTypes)[number];
export type TraceVisualStatus = "running" | "success" | "error" | "unset";
export type TraceVisualDiagnosticCode =
  | "clock_skew"
  | "duplicate_span_id"
  | "duration_mismatch"
  | "invalid_timestamp"
  | "missing_parent"
  | "missing_root"
  | "parent_cycle"
  | "root_has_parent";

export type TraceVisualDiagnostic = {
  code: TraceVisualDiagnosticCode;
  spanId?: string;
  relatedSpanId?: string;
};

export type TraceVisualNode = {
  id: string;
  parentId?: string;
  children: string[];
  depth: number;
  type: TraceVisualType;
  status: TraceVisualStatus;
  startMs: number;
  endMs: number;
  durationMs: number;
  isOrphan: boolean;
  isClockSkewed: boolean;
  span: TraceSpan;
};

export type TraceVisualEdge = {
  id: string;
  kind: "parent" | "link";
  sourceId: string;
  targetId?: string;
  linkedTraceId?: string;
  linkedSpanId?: string;
};

export type TraceVisualizationModel = {
  nodesById: Map<string, TraceVisualNode>;
  roots: string[];
  detachedRoots: string[];
  rows: string[];
  parentEdges: TraceVisualEdge[];
  linkEdges: TraceVisualEdge[];
  traceStartMs: number;
  traceEndMs: number;
  totalDurationMs: number;
  diagnostics: TraceVisualDiagnostic[];
};

export type TraceTimelineTick = {
  elapsedMs: number;
  ratio: number;
  label: string;
};

export type TraceTimelineBarGeometry = {
  leftRatio: number;
  widthRatio: number;
  minWidthPx: number;
};

const timestampDurationToleranceMs = 1;

export function traceVisualType(span: TraceSpan, rootSpanId?: string): TraceVisualType {
  if (span.peerAgentId) return "peer";
  const toolKind = span.toolKind?.trim().toLowerCase();
  if (span.mcpServer || toolKind === "mcp") return "mcp";
  const kind = span.openInferenceKind?.trim().toUpperCase();
  if (kind === "LLM") return "llm";
  if (kind === "RETRIEVER") return "retriever";
  if (kind === "TOOL" || toolKind) return "tool";
  if (span.spanId === rootSpanId || kind === "AGENT" || kind === "CHAIN") return "agent";
  return "unknown";
}

export function traceVisualStatus(span: TraceSpan): TraceVisualStatus {
  if (span.statusCode === "error") return "error";
  if (!span.endedAt) return "running";
  if (span.statusCode === "ok") return "success";
  return "unset";
}

export function buildTraceVisualizationModel(
  detail: TraceDetail,
  nowMs = Date.now(),
): TraceVisualizationModel {
  const diagnostics: TraceVisualDiagnostic[] = [];
  const spansById = new Map<string, TraceSpan>();
  for (const span of detail.spans) {
    if (spansById.has(span.spanId)) {
      diagnostics.push({ code: "duplicate_span_id", spanId: span.spanId });
      continue;
    }
    spansById.set(span.spanId, span);
  }

  const spans = [...spansById.values()];
  const validStarts = spans.map((span) => parseTimestamp(span.startedAt)).filter(isNumber);
  const summaryStart = parseTimestamp(detail.summary.startedAt);
  const startCandidates = [...validStarts, ...(summaryStart === undefined ? [] : [summaryStart])];
  const traceStartMs = startCandidates.length ? Math.min(...startCandidates) : nowMs;
  const validEnds = spans.map((span) => parseTimestamp(span.endedAt)).filter(isNumber);
  const summaryEnd = parseTimestamp(detail.summary.endedAt);
  const inferredEnd =
    detail.summary.status === "running"
      ? Math.max(nowMs, ...validEnds, traceStartMs)
      : Math.max(...validEnds, traceStartMs);
  const traceEndMs = summaryEnd ?? inferredEnd;
  const totalDurationMs = Math.max(1, traceEndMs - traceStartMs);
  if (summaryStart === undefined) diagnostics.push({ code: "invalid_timestamp" });
  if (summaryEnd !== undefined && summaryEnd < traceStartMs)
    diagnostics.push({ code: "clock_skew" });

  const preferredRootId = detail.rootSpan?.spanId ?? detail.summary.rootSpanId;
  if (preferredRootId && !spansById.has(preferredRootId)) {
    diagnostics.push({ code: "missing_root", relatedSpanId: preferredRootId });
  }

  const nodesById = new Map<string, TraceVisualNode>();
  for (const span of spans) {
    const parsedStart = parseTimestamp(span.startedAt);
    if (parsedStart === undefined) {
      diagnostics.push({ code: "invalid_timestamp", spanId: span.spanId });
    }
    const startMs = parsedStart ?? traceStartMs;
    const parsedEnd = parseTimestamp(span.endedAt);
    if (span.endedAt && parsedEnd === undefined) {
      diagnostics.push({ code: "invalid_timestamp", spanId: span.spanId });
    }
    const isClockSkewed = parsedEnd !== undefined && parsedEnd < startMs;
    const endMs =
      parsedEnd === undefined ? Math.max(nowMs, startMs) : isClockSkewed ? startMs : parsedEnd;
    if (isClockSkewed) diagnostics.push({ code: "clock_skew", spanId: span.spanId });
    const timestampDuration = endMs - startMs;
    if (
      span.durationMs !== null &&
      parsedEnd !== undefined &&
      !isClockSkewed &&
      Math.abs(span.durationMs - timestampDuration) > timestampDurationToleranceMs
    ) {
      diagnostics.push({ code: "duration_mismatch", spanId: span.spanId });
    }
    nodesById.set(span.spanId, {
      id: span.spanId,
      children: [],
      depth: 0,
      type: traceVisualType(span, preferredRootId),
      status: traceVisualStatus(span),
      startMs,
      endMs,
      durationMs: timestampDuration,
      isOrphan: false,
      isClockSkewed,
      span,
    });
  }

  const parentById = new Map<string, string | undefined>();
  const detachedIds = new Set<string>();
  for (const node of nodesById.values()) {
    const parentId = node.span.parentSpanId;
    if (!parentId) {
      parentById.set(node.id, undefined);
      continue;
    }
    if (!nodesById.has(parentId)) {
      parentById.set(node.id, undefined);
      detachedIds.add(node.id);
      diagnostics.push({ code: "missing_parent", spanId: node.id, relatedSpanId: parentId });
      continue;
    }
    parentById.set(node.id, parentId);
  }

  const visitState = new Map<string, "visiting" | "visited">();
  const breakCycles = (spanId: string) => {
    if (visitState.get(spanId) === "visited") return;
    visitState.set(spanId, "visiting");
    const parentId = parentById.get(spanId);
    if (parentId) {
      if (visitState.get(parentId) === "visiting") {
        parentById.set(spanId, undefined);
        detachedIds.add(spanId);
        diagnostics.push({ code: "parent_cycle", spanId, relatedSpanId: parentId });
      } else {
        breakCycles(parentId);
      }
    }
    visitState.set(spanId, "visited");
  };
  for (const node of sortNodes(nodesById.values())) breakCycles(node.id);

  if (preferredRootId && nodesById.has(preferredRootId) && parentById.get(preferredRootId)) {
    const parentId = parentById.get(preferredRootId);
    parentById.set(preferredRootId, undefined);
    diagnostics.push({ code: "root_has_parent", spanId: preferredRootId, relatedSpanId: parentId });
  }

  for (const node of nodesById.values()) {
    node.parentId = parentById.get(node.id);
    node.isOrphan = detachedIds.has(node.id);
    if (node.parentId) nodesById.get(node.parentId)?.children.push(node.id);
  }
  for (const node of nodesById.values()) {
    node.children.sort((leftId, rightId) =>
      compareNodes(nodesById.get(leftId)!, nodesById.get(rightId)!),
    );
  }

  const naturalRoots = sortNodes(nodesById.values()).filter((node) => !node.parentId);
  const detachedRoots = naturalRoots
    .filter((node) => detachedIds.has(node.id))
    .map((node) => node.id);
  const regularRoots = naturalRoots
    .filter((node) => !detachedIds.has(node.id))
    .map((node) => node.id);
  const roots =
    preferredRootId && regularRoots.includes(preferredRootId)
      ? [preferredRootId, ...regularRoots.filter((id) => id !== preferredRootId), ...detachedRoots]
      : [...regularRoots, ...detachedRoots];
  const rows: string[] = [];
  const visitedRows = new Set<string>();
  const appendRows = (spanId: string, depth: number) => {
    if (visitedRows.has(spanId)) return;
    visitedRows.add(spanId);
    const node = nodesById.get(spanId);
    if (!node) return;
    node.depth = depth;
    rows.push(spanId);
    for (const childId of node.children) appendRows(childId, depth + 1);
  };
  for (const rootId of roots) appendRows(rootId, 0);
  for (const node of sortNodes(nodesById.values())) appendRows(node.id, 0);

  const parentEdges = sortNodes(nodesById.values()).flatMap<TraceVisualEdge>((node) =>
    node.parentId
      ? [
          {
            id: `parent:${node.parentId}:${node.id}`,
            kind: "parent",
            sourceId: node.parentId,
            targetId: node.id,
          },
        ]
      : [],
  );
  const linkEdges = [...detail.links].sort(compareLinks).flatMap<TraceVisualEdge>((link) => {
    if (!nodesById.has(link.spanId)) return [];
    const localTarget =
      link.linkedTraceId === detail.summary.traceId && nodesById.has(link.linkedSpanId);
    return [
      {
        id: `link:${link.spanId}:${link.linkedTraceId}:${link.linkedSpanId}`,
        kind: "link",
        sourceId: link.spanId,
        targetId: localTarget ? link.linkedSpanId : undefined,
        linkedTraceId: link.linkedTraceId,
        linkedSpanId: link.linkedSpanId,
      },
    ];
  });

  return {
    nodesById,
    roots,
    detachedRoots,
    rows,
    parentEdges,
    linkEdges,
    traceStartMs,
    traceEndMs,
    totalDurationMs,
    diagnostics,
  };
}

export function buildTraceTimelineTicks(totalDurationMs: number): TraceTimelineTick[] {
  const duration = Math.max(1, totalDurationMs);
  const interval = traceTimelineTickInterval(duration);
  const ticks: TraceTimelineTick[] = [];
  for (let elapsedMs = 0; elapsedMs <= duration; elapsedMs += interval) {
    ticks.push({ elapsedMs, ratio: elapsedMs / duration, label: formatTick(elapsedMs) });
  }
  const last = ticks.at(-1);
  if (!last || last.elapsedMs !== duration) {
    ticks.push({ elapsedMs: duration, ratio: 1, label: formatTick(duration) });
  }
  return ticks;
}

export function traceTimelineTickInterval(totalDurationMs: number): number {
  if (totalDurationMs < 1_000) return 100;
  if (totalDurationMs <= 10_000) return 1_000;
  if (totalDurationMs <= 60_000) return 5_000;
  if (totalDurationMs <= 600_000) return 30_000;
  return 60_000;
}

export function traceTimelineBarGeometry(
  node: TraceVisualNode,
  model: Pick<TraceVisualizationModel, "traceStartMs" | "totalDurationMs">,
): TraceTimelineBarGeometry {
  return {
    leftRatio: (node.startMs - model.traceStartMs) / model.totalDurationMs,
    widthRatio: Math.max(0, node.endMs - node.startMs) / model.totalDurationMs,
    minWidthPx: 4,
  };
}

export function getTraceFocusIds(
  model: TraceVisualizationModel,
  selectedSpanId?: string,
): Set<string> {
  const focused = new Set<string>();
  if (!selectedSpanId || !model.nodesById.has(selectedSpanId)) return focused;
  addAncestors(model, selectedSpanId, focused);
  for (const childId of model.nodesById.get(selectedSpanId)?.children ?? []) focused.add(childId);
  return focused;
}

export function getVisibleTraceRows(
  model: TraceVisualizationModel,
  options: {
    activeTypes?: ReadonlySet<TraceVisualType>;
    collapsedIds?: ReadonlySet<string>;
    errorsOnly?: boolean;
    selectedSpanId?: string;
  } = {},
): string[] {
  const activeTypes = options.activeTypes ?? new Set(traceVisualTypes);
  const collapsedIds = options.collapsedIds ?? new Set<string>();
  const included = new Set<string>();
  const preserveThroughCollapse = new Set<string>();

  if (options.errorsOnly) {
    for (const node of model.nodesById.values()) {
      if (node.status !== "error") continue;
      addAncestors(model, node.id, included);
      addAncestors(model, node.id, preserveThroughCollapse);
      for (const childId of node.children) {
        included.add(childId);
        preserveThroughCollapse.add(childId);
      }
    }
  } else {
    for (const node of model.nodesById.values()) {
      if (activeTypes.has(node.type)) addAncestors(model, node.id, included);
      if (node.status === "error" || node.type === "peer") {
        addAncestors(model, node.id, preserveThroughCollapse);
      }
    }
  }
  if (options.selectedSpanId && model.nodesById.has(options.selectedSpanId)) {
    addAncestors(model, options.selectedSpanId, included);
    addAncestors(model, options.selectedSpanId, preserveThroughCollapse);
  }

  return model.rows.filter((spanId) => {
    if (!included.has(spanId)) return false;
    let parentId = model.nodesById.get(spanId)?.parentId;
    while (parentId) {
      if (collapsedIds.has(parentId) && !preserveThroughCollapse.has(spanId)) return false;
      parentId = model.nodesById.get(parentId)?.parentId;
    }
    return true;
  });
}

export function getDefaultCollapsedTraceNodes(model: TraceVisualizationModel): Set<string> {
  if (model.rows.length <= 100) return new Set();
  return new Set(
    model.rows.filter((spanId) => {
      const node = model.nodesById.get(spanId);
      return node?.type === "agent" && node.children.length > 0;
    }),
  );
}

function addAncestors(model: TraceVisualizationModel, spanId: string, target: Set<string>) {
  let currentId: string | undefined = spanId;
  while (currentId && !target.has(currentId)) {
    target.add(currentId);
    currentId = model.nodesById.get(currentId)?.parentId;
  }
}

function parseTimestamp(value?: string | null): number | undefined {
  if (!value) return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function isNumber(value: number | undefined): value is number {
  return value !== undefined;
}

function sortNodes(nodes: Iterable<TraceVisualNode>): TraceVisualNode[] {
  return [...nodes].sort(compareNodes);
}

function compareNodes(left: TraceVisualNode, right: TraceVisualNode): number {
  return left.startMs - right.startMs || left.id.localeCompare(right.id);
}

function compareLinks(left: TraceLink, right: TraceLink): number {
  return `${left.spanId}:${left.linkedTraceId}:${left.linkedSpanId}`.localeCompare(
    `${right.spanId}:${right.linkedTraceId}:${right.linkedSpanId}`,
  );
}

function formatTick(elapsedMs: number): string {
  if (elapsedMs === 0) return "0";
  if (elapsedMs < 1_000) return `${Math.round(elapsedMs)} ms`;
  if (elapsedMs < 60_000) return `${Number((elapsedMs / 1_000).toFixed(1))} s`;
  return `${Number((elapsedMs / 60_000).toFixed(1))} min`;
}
