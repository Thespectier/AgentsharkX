import type { QueryClient } from "@tanstack/react-query";

import type { UnifiedEvent } from "../types";
import type { TraceSummary } from "../generated/api-client";

const overviewQueries = [["overview"]] as const;
const gatewayQueries = [
  ["connect-summary"],
  ["connect-llm-configuration"],
  ["connect-mcp-configuration"],
  ["connect-traffic-configuration"],
  ["protect-gateway-policy-configuration"],
] as const;
const guardQueries = [["monitored-agents"], ["trust-controls"], ["protect-approvals"]] as const;
const auditQueries = [["audit"], ["audit-event"]] as const;

async function invalidate(queryClient: QueryClient, prefixes: readonly (readonly string[])[]) {
  await Promise.all(
    prefixes.map((queryKey) => queryClient.invalidateQueries({ queryKey: [...queryKey] })),
  );
}

export async function synchronizeAgentGuardData(queryClient: QueryClient) {
  await invalidate(queryClient, [...overviewQueries, ...guardQueries, ...auditQueries]);
}

export async function synchronizeLiveEvent(queryClient: QueryClient, event: UnifiedEvent) {
  const sourceQueries = event.source === "agentgateway" ? gatewayQueries : guardQueries;
  await invalidate(queryClient, [...overviewQueries, ...auditQueries, ...sourceQueries]);
}

export async function synchronizeTraceUpdate(queryClient: QueryClient, summary: TraceSummary) {
  await invalidate(queryClient, [
    ["audit-traces"],
    ["audit-trace", summary.traceId],
    ["audit-trace-span", summary.traceId],
  ]);
}

export async function synchronizeStreamReset(queryClient: QueryClient) {
  await queryClient.invalidateQueries({
    predicate: (query) => query.queryKey[0] !== "admin-session",
  });
}
