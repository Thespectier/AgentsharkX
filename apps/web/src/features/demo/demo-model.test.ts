import { describe, expect, it } from "vitest";

import type { DemoApproval } from "../../generated/api-client";
import { demoRuns } from "../../mocks/data";
import { clampDemoDelay, demoApprovalRecord, demoMetricComparisons } from "./demo-model";

describe("Demo Lab view model", () => {
  it("preserves absent observed metrics as Pending instead of zero", () => {
    const run = { ...demoRuns[0], observedMetrics: null };
    const metrics = demoMetricComparisons(run);

    expect(metrics).toHaveLength(8);
    expect(metrics.every((metric) => metric.display === "Pending")).toBe(true);
    expect(metrics.every((metric) => metric.matches === null)).toBe(true);
  });

  it("keeps a reported zero distinct from an absent value", () => {
    const run = {
      ...demoRuns[0],
      observedMetrics: {
        llmCalls: 0,
        mcpCalls: 0,
        localToolCalls: 0,
        a2aCalls: 0,
        humanChecks: 0,
        errorCount: 0,
      },
    };
    const metrics = demoMetricComparisons(run);

    expect(metrics[0]).toMatchObject({ display: "0", observed: 0, matches: false });
    expect(metrics[4]).toMatchObject({ display: "0", observed: 0, matches: true });
  });

  it("adapts only a pending AgentGuard ticket to the shared approval contract", () => {
    const source = { ...demoRuns[2].approval!, status: "pending" };
    expect(demoApprovalRecord(source)).toMatchObject({
      id: source.ticketId,
      upstreamId: source.upstreamId,
      source: "agentguard",
      fetchedAt: source.fetchedAt,
      rawRef: source.rawRef,
      sessionId: source.sessionId,
      status: "pending",
    });
    expect(source.ticketId).not.toBe(source.upstreamId);
    expect(
      demoApprovalRecord({ ...source, source: "other" } as unknown as DemoApproval),
    ).toBeUndefined();
    expect(
      demoApprovalRecord({
        ...source,
        rawRef: { source: "agentgateway", id: source.rawRef.id },
      }),
    ).toBeUndefined();
    expect(demoApprovalRecord({ ...source, status: "approved" })).toBeUndefined();
  });

  it("bounds the only numeric scenario parameter", () => {
    expect(clampDemoDelay(-1)).toBe(0);
    expect(clampDemoDelay(701.4)).toBe(701);
    expect(clampDemoDelay(9_999)).toBe(2_000);
  });
});
