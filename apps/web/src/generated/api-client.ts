// Generated from api/openapi.yaml by scripts/generate-api-client.mjs. Do not edit.

export type Source = "agentgateway" | "agentguard";

export type LoginRequest = { token: string };

export type Meta = {
  source?: Source;
  sourceVersion?: string;
  fetchedAt: string;
  stale: boolean;
  partial?: boolean;
  sourceFailures?: Array<SourceFailure>;
};

export type SourceFailure = { source: Source; code: string; message: string };

export type DataEnvelope = { data: unknown; meta: Meta };

export type HealthSource = {
  source: Source;
  label: string;
  status: "healthy" | "degraded" | "down" | "unknown";
  version?: string;
  latencyMs: number | null;
  checkedAt: string;
  message?: string;
};

export type HealthEnvelope = { data: Array<HealthSource>; meta: Meta };

export type Capability = {
  id: string;
  source: Source;
  status: "supported" | "partial" | "link-out" | "unavailable";
  checkedAt: string;
  reason?: string;
};

export type CapabilitiesEnvelope = { data: Array<Capability>; meta: Meta };

export type SetupStep = { id: string; label: string; complete: boolean; command?: string };

export type Setup = { complete: boolean; steps: Array<SetupStep> };

export type Metric = {
  id: string;
  label: string;
  source: Source;
  value: number;
  format: "integer" | "percent" | "duration" | "currency";
  delta: number;
  trend: "up" | "down" | "flat";
  tone: "default" | "success" | "warning" | "danger";
  context: string;
};

export type TrendPoint = {
  time: string;
  requests: number;
  latency: number;
  errors: number;
  denied: number;
};

export type OverviewData = {
  environment: string;
  mode: "health-only" | "operational";
  health: Array<HealthSource>;
  metrics: Array<Metric>;
  trend: Array<TrendPoint>;
  events: Array<UnifiedEvent>;
  setup: Setup;
};

export type OverviewEnvelope = { data: OverviewData; meta: Meta };

export type LabelUpdate = {
  boundary?: string | null;
  sensitivity?: string | null;
  integrity?: string | null;
  tags?: Array<string>;
};

export type DetectionRequest = { resourceIds?: Array<string>; useLlm?: boolean };

export type RuleSource = { source: string };

export type ApprovalAction = { note: string };

export type UnifiedEvent = {
  id: string;
  timestamp: string;
  source: Source;
  kind: "traffic" | "decision" | "approval" | "audit" | "health";
  severity: "info" | "low" | "medium" | "high" | "critical";
  subject?: { agentId?: string; principalId?: string; sessionId?: string };
  target?: { provider?: string; model?: string; tool?: string; resource?: string };
  phase?: string;
  action?: string;
  decision?: string;
  correlation?: { traceId?: string; sessionId?: string; verified: boolean };
  summary: string;
  rawRef: { source: string; id: string };
};

export type EventsEnvelope = {
  data: { items: Array<UnifiedEvent>; nextCursor?: string | null };
  meta: Meta;
};

export type ErrorEnvelope = {
  error: { code: string; message: string; source?: Source; requestId: string; retryable: boolean };
};

export const implementedOperations = {
  createAdminSession: { method: "POST", path: "/api/v1/auth/session" },
  getCapabilities: { method: "GET", path: "/api/v1/system/capabilities" },
  getOverview: { method: "GET", path: "/api/v1/overview" },
  getSystemHealth: { method: "GET", path: "/api/v1/system/health" },
  streamEvents: { method: "GET", path: "/api/v1/stream" },
} as const;

export interface OperationResponses {
  createAdminSession: undefined;
  getCapabilities: CapabilitiesEnvelope;
  getOverview: OverviewEnvelope;
  getSystemHealth: HealthEnvelope;
  streamEvents: string;
}

export interface OperationBodies {
  createAdminSession: LoginRequest;
}

export type ImplementedOperationId = keyof typeof implementedOperations;
export type OperationResponse<K extends ImplementedOperationId> = OperationResponses[K];
export type OperationBody<K extends keyof OperationBodies> = OperationBodies[K];
