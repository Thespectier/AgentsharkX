export type Source = "agentgateway" | "agentguard";
export type HealthStatus = "healthy" | "connecting" | "degraded" | "down" | "unknown";
export type Severity = "info" | "low" | "medium" | "high" | "critical";
export type Scenario = "normal" | "empty" | "loading" | "partial" | "error";

export interface SourceHealth {
  source: Source;
  label: string;
  status: HealthStatus;
  version: string;
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
  raw: Record<string, unknown>;
}

export interface OverviewData {
  environment: string;
  health: SourceHealth[];
  metrics: Metric[];
  trend: TrendPoint[];
  events: UnifiedEvent[];
  setup: {
    complete: boolean;
    steps: Array<{ id: string; label: string; complete: boolean; command?: string }>;
  };
}

export interface GatewayRoute {
  id: string;
  listener: string;
  protocol: "HTTP" | "gRPC" | "A2A";
  hostname: string;
  target: string;
  status: HealthStatus;
  requests: number;
  source: "agentgateway";
}

export interface Provider {
  id: string;
  name: string;
  kind: string;
  models: number;
  requests: number;
  cost: number;
  status: HealthStatus;
  source: "agentgateway";
}

export interface Model {
  id: string;
  name: string;
  provider: string;
  inputTokens: number;
  outputTokens: number;
  p95LatencyMs: number;
  status: HealthStatus;
  source: "agentgateway";
}

export interface McpServer {
  id: string;
  name: string;
  transport: "sse" | "stdio" | "openapi";
  tools: number;
  policy: string;
  status: HealthStatus;
  source: "agentgateway";
}

export interface ConnectData {
  health: SourceHealth;
  summary: Array<{ label: string; value: number; healthy: number }>;
  routes: GatewayRoute[];
  providers: Provider[];
  models: Model[];
  mcpServers: McpServer[];
  consoleUrl: string;
  fetchedAt: string;
}

export interface AgentIdentity {
  id: string;
  name: string;
  framework: string;
  principal: string;
  trustLevel: "high" | "medium" | "low" | "unknown";
  status: HealthStatus;
  sessions: number;
  tools: number;
  lastActive: string;
  source: "agentguard";
}

export interface TrustResource {
  id: string;
  name: string;
  type: "Tool" | "Skill" | "MCP";
  ownerAgent: string;
  boundary: string;
  sensitivity: string;
  integrity: string;
  scanStatus: "clean" | "warning" | "pending" | "not-scanned";
  source: "agentguard";
}

export interface TrustData {
  agents: AgentIdentity[];
  resources: TrustResource[];
  trustDistribution: Array<{ name: string; value: number }>;
  scans: Array<{
    id: string;
    resource: string;
    type: "Skill" | "MCP";
    status: "clean" | "warning" | "running";
    findings: number;
    updatedAt: string;
  }>;
}

export interface Policy {
  id: string;
  name: string;
  source: Source;
  type: "Gateway Policy" | "Content Guardrail" | "Runtime Rule";
  scope: string;
  phase: string;
  action: string;
  status: "active" | "draft" | "disabled";
  matches24h: number;
  updatedAt: string;
}

export interface Approval {
  id: string;
  agentId: string;
  tool: string;
  risk: Severity;
  reason: string;
  requestedAt: string;
  phase: string;
  source: "agentguard";
}

export interface ProtectData {
  policies: Policy[];
  approvals: Approval[];
  coverage: Array<{ phase: string; active: number; available: number }>;
}

export interface AuditData {
  metrics: Metric[];
  trend: TrendPoint[];
  events: UnifiedEvent[];
  sessions: Array<{
    id: string;
    agentId: string;
    principal: string;
    events: number;
    denies: number;
    startedAt: string;
    status: "active" | "closed";
    source: "agentguard";
  }>;
}
