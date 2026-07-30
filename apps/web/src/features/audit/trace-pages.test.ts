import { describe, expect, it } from "vitest";

import { requestOperation } from "../../lib/api";
import { demoTraceIds, mainTraceId, mockTraceSummaries } from "../../mocks/data";
import { tracePageTestHelpers } from "./trace-pages";
import type { TraceSummary } from "../../generated/api-client";

describe("Trace list query state", () => {
  it("round-trips stable filters and opaque cursor history through the URL", () => {
    const parsed = tracePageTestHelpers.traceFiltersFromSearch(
      '?cursor=opaque-next&trace_history=["","opaque-first"]&status=failed&agent_id=support-agent&has_error=true&has_a2a=false&started_after=2026-07-30T08%3A00&query=task-42',
    );

    expect(parsed).toMatchObject({
      cursor: "opaque-next",
      cursorHistory: ["", "opaque-first"],
      status: "failed",
      agentId: "support-agent",
      hasError: "true",
      hasA2A: "false",
      startedAfter: "2026-07-30T08:00",
      query: "task-42",
    });
    expect(tracePageTestHelpers.buildSearch(parsed, "normal")).toContain(
      "trace_history=%5B%22%22%2C%22opaque-first%22%5D",
    );
    expect(tracePageTestHelpers.traceRequestQuery(parsed)).toMatchObject({
      started_after: "2026-07-30T00:00:00.000Z",
      agent_id: "support-agent",
      has_error: "true",
    });

    const longHistory = Array.from({ length: 25 }, (_, index) => `opaque-${index}`);
    const longHistorySearch = new URLSearchParams({ trace_history: JSON.stringify(longHistory) });
    expect(
      tracePageTestHelpers.traceFiltersFromSearch(`?${longHistorySearch}`).cursorHistory,
    ).toEqual(longHistory);
  });

  it("distinguishes a running duration from a missing verified duration", () => {
    expect(tracePageTestHelpers.formatTraceDuration(undefined, "running")).toBe("Running");
    expect(tracePageTestHelpers.formatTraceDuration(undefined, "unknown")).toBe("Not verified");
    expect(tracePageTestHelpers.formatTraceDuration(1_250, "succeeded")).toBe("1.25 s");
  });

  it("holds new first-page inserts while updating visible summaries in place", () => {
    const original = { traceId: "11111111111111111111111111111111", spanCount: 2 } as TraceSummary;
    const updated = { ...original, spanCount: 3 };
    const inserted = { traceId: "22222222222222222222222222222222" } as TraceSummary;
    const displayed = { items: [original], nextCursor: null, total: 1 };
    const incoming = { items: [inserted, updated], nextCursor: null, total: 2 };

    const result = tracePageTestHelpers.reconcileTracePage(displayed, incoming, true);

    expect(result.displayed.items).toEqual([updated]);
    expect(result.pending).toBe(incoming);
    expect(result.newCount).toBe(1);
    expect(
      tracePageTestHelpers.reconcileTracePage(result.displayed, incoming, false).displayed.items,
    ).toEqual([inserted, updated]);
  });
});

describe("Trace mock contract", () => {
  it("uses unique W3C Trace identities", () => {
    expect(new Set(mockTraceSummaries.map((trace) => trace.traceId)).size).toBe(
      mockTraceSummaries.length,
    );
    expect(mockTraceSummaries.every((trace) => /^[0-9a-f]{32}$/.test(trace.traceId))).toBe(true);
  });

  it("applies filters and stable cursor pagination to generated response types", async () => {
    const a2a = await requestOperation("listAuditTraces", {
      query: { limit: 25, has_a2a: "true" },
    });
    expect(a2a.data.items).toHaveLength(4);
    expect(a2a.data.items.map((trace) => trace.traceId)).toEqual(
      expect.arrayContaining([mainTraceId, ...Object.values(demoTraceIds)]),
    );
    expect(a2a.data.items.every((trace) => trace.a2aCalls === 1)).toBe(true);

    const first = await requestOperation("listAuditTraces", { query: { limit: 2 } });
    expect(first.data.items).toHaveLength(2);
    expect(first.data.nextCursor).toBe("mock:2");
    const second = await requestOperation("listAuditTraces", {
      query: { cursor: first.data.nextCursor ?? undefined, limit: 2 },
    });
    expect(second.data.items).toHaveLength(2);
    expect(second.data.items.map((trace) => trace.traceId)).not.toEqual(
      first.data.items.map((trace) => trace.traceId),
    );
    expect(second.data.total).toBe(first.data.total);

    const exactIdentity = await requestOperation("listAuditTraces", {
      query: { limit: 25, session_id: "ses_rg_84f2", task_id: "task-research-042" },
    });
    expect(exactIdentity.data.items).toHaveLength(1);
    expect(exactIdentity.data.items[0]?.traceId).toBe(mainTraceId);

    const exclusiveBefore = await requestOperation("listAuditTraces", {
      query: { limit: 100, started_before: "2026-07-30T07:58:00Z" },
    });
    expect(exclusiveBefore.data.items.map((trace) => trace.traceId)).not.toContain(mainTraceId);
  });

  it("keeps content out of Trace detail and returns it only for one selected Span", async () => {
    const detail = await requestOperation("getAuditTrace", {
      path: { traceId: mainTraceId },
    });
    expect(detail.data.links).toHaveLength(1);
    expect(JSON.stringify(detail)).not.toContain("Plan the verified inventory research task");

    const span = await requestOperation("getAuditTraceSpan", {
      path: { traceId: mainTraceId, spanId: "2000000000000002" },
    });
    expect(span.data.span.contentState).toBe("captured");
    expect(span.data.payloads).toHaveLength(2);
    expect(JSON.stringify(span)).toContain("Plan the verified inventory research task");
  });
});
