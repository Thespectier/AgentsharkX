import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Metric, SourceHealth, TrendPoint, UnifiedEvent } from "../types";
import { classifyLiveFlowEvent, LiveFlow, summarizeLiveFlow } from "./dashboard-motion";

const health: SourceHealth[] = [
  {
    source: "agentgateway",
    label: "agentgateway",
    status: "healthy",
    latencyMs: 12,
    checkedAt: "2026-07-21T12:42:10Z",
    message: "ready",
  },
  {
    source: "agentguard",
    label: "AgentGuard",
    status: "degraded",
    latencyMs: 22,
    checkedAt: "2026-07-21T12:42:10Z",
    message: "partial",
  },
];

const metrics = (requests: number, decisions: number): Metric[] => [
  {
    id: "gateway-requests",
    label: "Gateway requests",
    source: "agentgateway",
    value: requests,
    format: "integer",
    delta: 0,
    trend: "flat",
    tone: "default",
    context: "Last 60 minutes",
  },
  {
    id: "guard-decisions",
    label: "Guard decisions",
    source: "agentguard",
    value: decisions,
    format: "integer",
    delta: 0,
    trend: "flat",
    tone: "default",
    context: "Last 60 minutes",
  },
];

const trend = (errors: number, denied: number): TrendPoint[] => [
  {
    time: "2026-07-28T01:00:00Z",
    requests: 9,
    latency: 20,
    latencySamples: 1,
    errors,
    denied,
  },
];

function event(overrides: Partial<UnifiedEvent>): UnifiedEvent {
  return {
    id: "event-1",
    timestamp: "2026-07-28T01:00:00Z",
    source: "agentgateway",
    kind: "traffic",
    severity: "info",
    summary: "request completed",
    rawRef: { source: "agentgateway", id: "event-1" },
    ...overrides,
  };
}

describe("live activity flow", () => {
  it("derives every category from verified overview fields", () => {
    expect(summarizeLiveFlow(metrics(12, 7), trend(2, 3))).toEqual({
      gatewayRequests: 12,
      gatewayErrors: 2,
      guardDecisions: 7,
      guardDenied: 3,
    });
    expect(classifyLiveFlowEvent(event({ severity: "high" }))).toBe("gateway-error");
    expect(
      classifyLiveFlowEvent(
        event({
          source: "agentguard",
          decision: "DENY",
          rawRef: { source: "agentguard", id: "deny" },
        }),
      ),
    ).toBe("guard-denied");
    expect(classifyLiveFlowEvent(event({ kind: "health" }))).toBeUndefined();
  });

  it("rerenders source health and rolling counts when overview data changes", () => {
    const { rerender } = render(
      <LiveFlow
        events={[]}
        health={health}
        metrics={metrics(12, 7)}
        status="live"
        trend={trend(2, 3)}
      />,
    );
    const topology = screen.getByRole("img", { name: "Live agent traffic topology" });
    const requests = within(topology).getByText("Requests").parentElement;
    const decisions = within(topology).getByText("Decisions").parentElement;
    expect(requests).not.toBeNull();
    expect(decisions).not.toBeNull();
    expect(within(requests as HTMLElement).getByText("12 · Last 60m")).toBeVisible();
    expect(within(decisions as HTMLElement).getByText("7 · Last 60m")).toBeVisible();

    rerender(
      <LiveFlow
        events={[]}
        health={health}
        metrics={metrics(24, 11)}
        status="live"
        trend={trend(4, 5)}
      />,
    );
    expect(within(requests as HTMLElement).getByText("24 · Last 60m")).toBeVisible();
    expect(within(decisions as HTMLElement).getByText("11 · Last 60m")).toBeVisible();
  });
});
