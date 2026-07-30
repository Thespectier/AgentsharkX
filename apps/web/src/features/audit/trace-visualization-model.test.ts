import { describe, expect, it } from "vitest";

import type { TraceDetail, TraceSpan } from "../../generated/api-client";
import { mockTraceDetails, mainTraceId } from "../../mocks/data";
import {
  buildTraceTimelineTicks,
  buildTraceVisualizationModel,
  getVisibleTraceRows,
  traceTimelineBarGeometry,
  traceTimelineTickInterval,
  traceVisualType,
} from "./trace-visualization-model";

const traceId = "99999999999999999999999999999999";
const base = mockTraceDetails[mainTraceId];

function span(spanId: string, overrides: Partial<TraceSpan> = {}): TraceSpan {
  return {
    ...base.spans[0]!,
    traceId,
    spanId,
    name: `Span ${spanId}`,
    startedAt: "2026-07-30T08:00:00Z",
    endedAt: "2026-07-30T08:00:01Z",
    durationMs: 1_000,
    ...overrides,
  };
}

function detail(spans: TraceSpan[], overrides: Partial<TraceDetail> = {}): TraceDetail {
  return {
    ...base,
    summary: {
      ...base.summary,
      traceId,
      rootSpanId: spans[0]?.spanId,
      startedAt: "2026-07-30T08:00:00Z",
      endedAt: "2026-07-30T08:00:10Z",
      durationMs: 10_000,
      spanCount: spans.length,
    },
    rootSpan: spans[0],
    spans,
    links: [],
    totalSpans: spans.length,
    ...overrides,
  };
}

describe("Trace visualization model", () => {
  it("builds a stable depth-first tree with explicit parent edges only", () => {
    const root = span("0000000000000001", { endedAt: "2026-07-30T08:00:10Z", durationMs: 10_000 });
    const late = span("0000000000000003", {
      parentSpanId: root.spanId,
      startedAt: "2026-07-30T08:00:04Z",
    });
    const early = span("0000000000000002", {
      parentSpanId: root.spanId,
      startedAt: "2026-07-30T08:00:02Z",
    });
    const child = span("0000000000000004", {
      parentSpanId: early.spanId,
      startedAt: "2026-07-30T08:00:03Z",
    });
    const model = buildTraceVisualizationModel(detail([root, late, child, early]));

    expect(model.rows).toEqual([root.spanId, early.spanId, child.spanId, late.spanId]);
    expect(model.parentEdges.map((edge) => [edge.sourceId, edge.targetId])).toEqual([
      [root.spanId, early.spanId],
      [early.spanId, child.spanId],
      [root.spanId, late.spanId],
    ]);
    expect(model.nodesById.get(child.spanId)?.depth).toBe(2);
  });

  it("groups missing parents as detached without inventing an edge", () => {
    const root = span("0000000000000001");
    const orphan = span("0000000000000002", { parentSpanId: "ffffffffffffffff" });
    const model = buildTraceVisualizationModel(detail([root, orphan]));

    expect(model.detachedRoots).toEqual([orphan.spanId]);
    expect(model.nodesById.get(orphan.spanId)?.isOrphan).toBe(true);
    expect(model.parentEdges).toHaveLength(0);
    expect(model.diagnostics).toContainEqual(
      expect.objectContaining({ code: "missing_parent", spanId: orphan.spanId }),
    );
  });

  it("breaks a parent cycle deterministically and keeps every row reachable", () => {
    const first = span("0000000000000001", { parentSpanId: "0000000000000002" });
    const second = span("0000000000000002", { parentSpanId: first.spanId });
    const model = buildTraceVisualizationModel(detail([first, second], { rootSpan: undefined }));

    expect(model.rows).toHaveLength(2);
    expect(new Set(model.rows)).toEqual(new Set([first.spanId, second.spanId]));
    expect(model.parentEdges.length).toBeLessThanOrEqual(1);
    expect(model.diagnostics.some((item) => item.code === "parent_cycle")).toBe(true);
  });

  it("keeps the first duplicate Span record and reports the duplicate", () => {
    const first = span("0000000000000001", { name: "First" });
    const duplicate = span(first.spanId, { name: "Duplicate" });
    const model = buildTraceVisualizationModel(detail([first, duplicate]));

    expect(model.nodesById.get(first.spanId)?.span.name).toBe("First");
    expect(model.diagnostics).toContainEqual(
      expect.objectContaining({ code: "duplicate_span_id", spanId: first.spanId }),
    );
  });

  it("uses one global time baseline for parallel bars and enforces a visible minimum", () => {
    const root = span("0000000000000001", { endedAt: "2026-07-30T08:00:10Z", durationMs: 10_000 });
    const first = span("0000000000000002", {
      parentSpanId: root.spanId,
      startedAt: "2026-07-30T08:00:02Z",
      endedAt: "2026-07-30T08:00:06Z",
      durationMs: 4_000,
    });
    const second = span("0000000000000003", {
      parentSpanId: root.spanId,
      startedAt: "2026-07-30T08:00:04Z",
      endedAt: "2026-07-30T08:00:08Z",
      durationMs: 4_000,
    });
    const instant = span("0000000000000004", {
      parentSpanId: root.spanId,
      startedAt: "2026-07-30T08:00:09Z",
      endedAt: "2026-07-30T08:00:09Z",
      durationMs: 0,
    });
    const model = buildTraceVisualizationModel(detail([root, first, second, instant]));
    const firstBar = traceTimelineBarGeometry(model.nodesById.get(first.spanId)!, model);
    const secondBar = traceTimelineBarGeometry(model.nodesById.get(second.spanId)!, model);
    const instantBar = traceTimelineBarGeometry(model.nodesById.get(instant.spanId)!, model);

    expect(firstBar).toMatchObject({ leftRatio: 0.2, widthRatio: 0.4 });
    expect(secondBar).toMatchObject({ leftRatio: 0.4, widthRatio: 0.4 });
    expect(firstBar.leftRatio + firstBar.widthRatio).toBeGreaterThan(secondBar.leftRatio);
    expect(instantBar).toMatchObject({ widthRatio: 0, minWidthPx: 4 });
  });

  it("marks clock skew and uses a zero visual duration", () => {
    const skewed = span("0000000000000001", {
      startedAt: "2026-07-30T08:00:05Z",
      endedAt: "2026-07-30T08:00:04Z",
      durationMs: 1_000,
    });
    const model = buildTraceVisualizationModel(detail([skewed]));

    expect(model.nodesById.get(skewed.spanId)).toMatchObject({
      isClockSkewed: true,
      durationMs: 0,
    });
    expect(model.diagnostics.some((item) => item.code === "clock_skew")).toBe(true);
  });

  it("uses the current time for a running Span without fabricating clock skew", () => {
    const running = span("0000000000000001", {
      startedAt: "2026-07-30T08:00:00Z",
      endedAt: null,
      durationMs: null,
      statusCode: "unset",
    });
    const runningDetail = detail([running], {
      summary: {
        ...base.summary,
        traceId,
        rootSpanId: running.spanId,
        status: "running",
        completeness: "partial",
        startedAt: running.startedAt,
        endedAt: undefined,
        durationMs: undefined,
        spanCount: 1,
      },
    });
    const nowMs = Date.parse("2026-07-30T08:00:05Z");
    const model = buildTraceVisualizationModel(runningDetail, nowMs);

    expect(model.nodesById.get(running.spanId)).toMatchObject({
      status: "running",
      endMs: nowMs,
      durationMs: 5_000,
      isClockSkewed: false,
    });
    expect(model.traceEndMs).toBe(nowMs);
  });

  it("maps visual types in the specified priority order", () => {
    expect(traceVisualType(span("1", { peerAgentId: "peer", openInferenceKind: "LLM" }))).toBe(
      "peer",
    );
    expect(traceVisualType(span("2", { mcpServer: "server", openInferenceKind: "LLM" }))).toBe(
      "mcp",
    );
    expect(traceVisualType(span("3", { openInferenceKind: "LLM" }))).toBe("llm");
    expect(traceVisualType(span("4", { openInferenceKind: "RETRIEVER" }))).toBe("retriever");
    expect(traceVisualType(span("5", { openInferenceKind: "TOOL" }))).toBe("tool");
    expect(traceVisualType(span("6", { openInferenceKind: "CHAIN" }))).toBe("agent");
    expect(traceVisualType(span("7", { openInferenceKind: undefined }))).toBe("unknown");
  });

  it("keeps selected ancestors and error context visible through collapse", () => {
    const root = span("0000000000000001", { endedAt: "2026-07-30T08:00:10Z", durationMs: 10_000 });
    const branch = span("0000000000000002", {
      parentSpanId: root.spanId,
      openInferenceKind: "CHAIN",
    });
    const selected = span("0000000000000003", {
      parentSpanId: branch.spanId,
      openInferenceKind: "LLM",
    });
    const error = span("0000000000000004", { parentSpanId: root.spanId, statusCode: "error" });
    const errorChild = span("0000000000000005", { parentSpanId: error.spanId });
    const model = buildTraceVisualizationModel(detail([root, branch, selected, error, errorChild]));

    expect(
      getVisibleTraceRows(model, {
        collapsedIds: new Set([root.spanId, branch.spanId]),
        selectedSpanId: selected.spanId,
      }),
    ).toEqual([root.spanId, branch.spanId, selected.spanId, error.spanId]);
    expect(getVisibleTraceRows(model, { errorsOnly: true })).toEqual([
      root.spanId,
      error.spanId,
      errorChild.spanId,
    ]);
  });

  it("creates adaptive, deterministic time ticks", () => {
    expect(traceTimelineTickInterval(800)).toBe(100);
    expect(traceTimelineTickInterval(6_000)).toBe(1_000);
    expect(traceTimelineTickInterval(30_000)).toBe(5_000);
    expect(traceTimelineTickInterval(300_000)).toBe(30_000);
    expect(traceTimelineTickInterval(900_000)).toBe(60_000);
    expect(buildTraceTimelineTicks(2_500).map((tick) => tick.label)).toEqual([
      "0",
      "1 s",
      "2 s",
      "2.5 s",
    ]);
  });
});
