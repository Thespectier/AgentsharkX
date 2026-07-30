import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import type { UnifiedEvent } from "../types";
import type { TraceSummary } from "../generated/api-client";
import {
  synchronizeAgentGuardData,
  synchronizeLiveEvent,
  synchronizeStreamReset,
  synchronizeTraceUpdate,
} from "./query-sync";

function event(source: UnifiedEvent["source"], kind: UnifiedEvent["kind"]): UnifiedEvent {
  return {
    id: `${source}:${kind}:1`,
    timestamp: "2026-07-23T00:00:00Z",
    source,
    kind,
    severity: "info",
    summary: "verified event",
    rawRef: { source, id: "1" },
  };
}

function seededClient() {
  const client = new QueryClient();
  for (const key of [
    "overview",
    "audit",
    "connect-summary",
    "monitored-agents",
    "trust-controls",
    "protect-approvals",
    "admin-session",
    "audit-traces",
  ]) {
    client.setQueryData([key], { value: key });
  }
  return client;
}

describe("query synchronization", () => {
  it("invalidates only the relevant source families for a live event", async () => {
    const client = seededClient();

    await synchronizeLiveEvent(client, event("agentguard", "decision"));

    expect(client.getQueryState(["overview"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["audit"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["monitored-agents"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["trust-controls"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["protect-approvals"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["connect-summary"])?.isInvalidated).toBe(false);
  });

  it("invalidates only Trace consumers for a summary-only Trace delivery", async () => {
    const client = seededClient();
    const summary = {
      traceId: "11111111111111111111111111111111",
    } as TraceSummary;
    client.setQueryData(["audit-trace", summary.traceId], { value: "detail" });
    client.setQueryData(["audit-trace-span", summary.traceId, "span"], { value: "span" });

    await synchronizeTraceUpdate(client, summary);

    expect(client.getQueryState(["audit-traces"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["audit-trace", summary.traceId])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["audit-trace-span", summary.traceId, "span"])?.isInvalidated).toBe(
      true,
    );
    expect(client.getQueryState(["overview"])?.isInvalidated).toBe(false);
    expect(client.getQueryState(["audit"])?.isInvalidated).toBe(false);
  });

  it("invalidates all AgentGuard consumers after a mutation", async () => {
    const client = seededClient();

    await synchronizeAgentGuardData(client);

    expect(client.getQueryState(["overview"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["audit"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["monitored-agents"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["trust-controls"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["protect-approvals"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["connect-summary"])?.isInvalidated).toBe(false);
  });

  it("invalidates every data query after a stream reset without refreshing auth", async () => {
    const client = seededClient();

    await synchronizeStreamReset(client);

    for (const key of [
      "overview",
      "audit",
      "connect-summary",
      "monitored-agents",
      "trust-controls",
      "protect-approvals",
    ]) {
      expect(client.getQueryState([key])?.isInvalidated).toBe(true);
    }
    expect(client.getQueryState(["admin-session"])?.isInvalidated).toBe(false);
  });
});
