import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import type { UnifiedEvent } from "../types";
import { synchronizeAgentGuardData, synchronizeLiveEvent } from "./query-sync";

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
    "trust-agents",
    "protect-approvals",
    "system-health",
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
    expect(client.getQueryState(["trust-agents"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["protect-approvals"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["connect-summary"])?.isInvalidated).toBe(false);
    expect(client.getQueryState(["system-health"])?.isInvalidated).toBe(false);
  });

  it("invalidates all AgentGuard consumers after a mutation", async () => {
    const client = seededClient();

    await synchronizeAgentGuardData(client);

    expect(client.getQueryState(["overview"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["audit"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["trust-agents"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["protect-approvals"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["connect-summary"])?.isInvalidated).toBe(false);
  });
});
