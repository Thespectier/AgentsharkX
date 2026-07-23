import type { QueryClient } from "@tanstack/react-query";

import type { UnifiedEvent } from "../types";

const overviewQueries = [["overview"]] as const;
const systemQueries = [["system-health"], ["system-capabilities"], ["system-diagnostics"]] as const;
const gatewayQueries = [
  ["connect-summary"],
  ["connect-setup"],
  ["connect-providers"],
  ["connect-models"],
  ["connect-mcp"],
  ["connect-routes"],
  ["connect-detail"],
] as const;
const guardQueries = [
  ["trust-agents"],
  ["trust-agent"],
  ["trust-resources"],
  ["trust-scans"],
  ["protect"],
  ["protect-approvals"],
] as const;
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
  await invalidate(queryClient, [
    ...overviewQueries,
    ...auditQueries,
    ...sourceQueries,
    ...(event.kind === "health" ? systemQueries : []),
  ]);
}
