import type {
  Approval,
  DemoApproval,
  DemoMetrics,
  DemoRun,
  TraceSummary,
} from "../../generated/api-client";
import { formatTraceDuration } from "../../lib/format";

export const demoMetricFields: Array<{ key: keyof DemoMetrics; label: string }> = [
  { key: "llmCalls", label: "LLM calls" },
  { key: "mcpCalls", label: "MCP calls" },
  { key: "localToolCalls", label: "Local tool calls" },
  { key: "a2aCalls", label: "A2A calls" },
  { key: "humanChecks", label: "Human checks" },
  { key: "errorCount", label: "Errors" },
];

export type DemoMetricComparison = {
  key: string;
  label: string;
  observed: number | null;
  expected: number | null;
  display: string;
  matches: boolean | null;
};

export function demoMetricComparisons(run: DemoRun, trace?: TraceSummary): DemoMetricComparison[] {
  const comparisons: DemoMetricComparison[] = demoMetricFields.map(({ key, label }) => {
    const observed = run.observedMetrics?.[key] ?? null;
    const expected = run.expectedMetrics[key];
    return {
      key,
      label,
      observed,
      expected,
      display: observed === null ? "Pending" : String(observed),
      matches: observed === null ? null : observed === expected,
    };
  });
  comparisons.push(
    {
      key: "tokens",
      label: "Tokens",
      observed: trace?.totalTokens ?? null,
      expected: null,
      display: trace ? String(trace.totalTokens) : "Pending",
      matches: trace ? true : null,
    },
    {
      key: "duration",
      label: "Duration",
      observed: trace?.durationMs ?? null,
      expected: null,
      display: trace ? formatTraceDuration(trace.durationMs, trace.status) : "Pending",
      matches: trace ? true : null,
    },
  );
  return comparisons;
}

export function demoApprovalRecord(approval?: DemoApproval): Approval | undefined {
  if (
    !approval ||
    approval.source !== "agentguard" ||
    approval.rawRef.source !== "agentguard" ||
    approval.status !== "pending"
  ) {
    return undefined;
  }
  return {
    id: approval.ticketId,
    upstreamId: approval.upstreamId,
    source: "agentguard",
    fetchedAt: approval.fetchedAt,
    rawRef: approval.rawRef,
    agentId: approval.agentId,
    agentUpstreamId: approval.agentUpstreamId,
    sessionId: approval.sessionId,
    eventId: approval.eventId,
    eventType: approval.eventType,
    tool: approval.tool,
    phase: approval.phase,
    action: approval.action,
    reason: approval.reason,
    riskScore: approval.riskScore,
    matchedRules: approval.matchedRules,
    status: "pending",
    createdAt: approval.createdAt,
  };
}

export function clampDemoDelay(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.min(2_000, Math.max(0, Math.round(value)));
}

export function createDemoRequestId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return `demo-${crypto.randomUUID()}`;
  }
  return `demo-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

export function formatDemoRunDuration(run: DemoRun): string {
  const start = Date.parse(run.startedAt ?? run.requestedAt);
  const end = run.completedAt ? Date.parse(run.completedAt) : Date.now();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return "Not verified";
  return formatTraceDuration(end - start);
}
