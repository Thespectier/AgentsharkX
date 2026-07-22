export type Source = "agentgateway" | "agentguard";
export type HealthStatus = "healthy" | "connecting" | "degraded" | "down" | "unknown";
export type Severity = "info" | "low" | "medium" | "high" | "critical";
export type Scenario = "normal" | "empty" | "loading" | "partial" | "error";

export interface SourceHealth {
  source: Source;
  label: string;
  status: HealthStatus;
  version?: string;
  latencyMs: number | null;
  message: string;
}

export interface ResponseMeta {
  source?: Source;
  sourceVersion?: string;
  fetchedAt: string;
  stale: boolean;
  partial?: boolean;
  sourceFailures?: Array<{
    source: Source;
    code: string;
    message: string;
  }>;
}

export interface Envelope<T> {
  data: T;
  meta: ResponseMeta;
}

export interface ApiFailure {
  error: {
    code: string;
    message: string;
    source?: Source;
    requestId: string;
    retryable: boolean;
  };
}

export interface Metric {
  id: string;
  label: string;
  source: Source;
  value: number;
  format: "integer" | "percent" | "duration" | "currency";
  delta: number;
  trend: "up" | "down" | "flat";
  tone: "default" | "success" | "warning" | "danger";
  context: string;
}

export interface TrendPoint {
  time: string;
  requests: number;
  latency: number;
  errors: number;
  denied: number;
}

export interface UnifiedEvent {
  id: string;
  timestamp: string;
  source: Source;
  kind: "traffic" | "decision" | "approval" | "audit" | "health";
  severity: Severity;
  subject?: {
    agentId?: string;
    principalId?: string;
    sessionId?: string;
  };
  target?: {
    provider?: string;
    model?: string;
    tool?: string;
    resource?: string;
  };
  phase?: string;
  action?: string;
  decision?: string;
  correlation?: {
    traceId?: string;
    sessionId?: string;
    verified: boolean;
  };
  summary: string;
  rawRef: {
    source: string;
    id: string;
  };
  raw?: Record<string, unknown>;
}

export interface OverviewData {
  environment: string;
  mode?: "health-only" | "operational";
  health: SourceHealth[];
  metrics: Metric[];
  trend: TrendPoint[];
  events: UnifiedEvent[];
  setup: {
    complete: boolean;
    steps: Array<{ id: string; label: string; complete: boolean; command?: string }>;
  };
}

export interface AuditData {
  metrics: Metric[];
  trend: TrendPoint[];
  events: UnifiedEvent[];
  sessions: Array<{
    id: string;
    upstreamId: string;
    agentId: string;
    agentUpstreamId: string;
    principal?: string;
    events: number;
    denies: number;
    lastSeen: string | null;
    status: "unknown";
    source: "agentguard";
    rawRef: { source: Source; id: string };
  }>;
}
