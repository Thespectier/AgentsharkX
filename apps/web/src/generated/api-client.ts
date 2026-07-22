// Generated from api/openapi.yaml by scripts/generate-api-client.mjs. Do not edit.

export type Source = "agentgateway" | "agentguard";

export type GatewaySource = "agentgateway";

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

export type RawRef = { source: Source; id: string };

export type GatewayProvider = {
  id: string;
  upstreamId?: string;
  source: GatewaySource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  kind: string;
  modelCount: number;
};

export type GatewayModel = {
  id: string;
  upstreamId?: string;
  source: GatewaySource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  kind: "direct" | "virtual";
  provider?: string;
  targetModel?: string;
  routing?: "weighted" | "failover" | "conditional";
  targets?: Array<string>;
};

export type GatewayMCPServer = {
  id: string;
  upstreamId?: string;
  source: GatewaySource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  transport: "sse" | "mcp" | "stdio" | "openapi";
  scope: string;
};

export type GatewayRoute = {
  id: string;
  upstreamId?: string;
  source: GatewaySource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  listener: string;
  protocol: string;
  port: number;
  hostnames: Array<string>;
  path?: string;
  targets: Array<string>;
  backendCount: number;
  unavailableBackendCount: number;
};

export type AnalyticsBucket = {
  start: string;
  requests: number;
  totalTokens: number;
  cost: number;
};

export type GatewayAnalytics = {
  status: "available" | "unavailable";
  reason?: string;
  requests: number | null;
  totalTokens: number | null;
  cost: number | null;
  bucketSeconds: number | null;
  buckets: Array<AnalyticsBucket>;
};

export type ConnectCount = {
  id: string;
  label: string;
  value: number | null;
  status: "configured" | "unavailable";
  reason?: string;
};

export type ConsoleLinks = {
  console?: string;
  rawConfig?: string;
  cel?: string;
  llmPlayground?: string;
  mcpPlayground?: string;
};

export type ConnectSummary = {
  health: HealthSource;
  counts: Array<ConnectCount>;
  analytics: GatewayAnalytics;
  links: ConsoleLinks;
};

export type ConnectSummaryEnvelope = { data: ConnectSummary; meta: Meta };

export type ProviderPage = {
  items: Array<GatewayProvider>;
  nextCursor: string | null;
  total: number;
};

export type ProviderPageEnvelope = { data: ProviderPage; meta: Meta };

export type ProviderEnvelope = { data: GatewayProvider; meta: Meta };

export type ModelPage = { items: Array<GatewayModel>; nextCursor: string | null; total: number };

export type ModelPageEnvelope = { data: ModelPage; meta: Meta };

export type ModelEnvelope = { data: GatewayModel; meta: Meta };

export type MCPPage = { items: Array<GatewayMCPServer>; nextCursor: string | null; total: number };

export type MCPPageEnvelope = { data: MCPPage; meta: Meta };

export type MCPEnvelope = { data: GatewayMCPServer; meta: Meta };

export type RoutePage = { items: Array<GatewayRoute>; nextCursor: string | null; total: number };

export type RoutePageEnvelope = { data: RoutePage; meta: Meta };

export type RouteEnvelope = { data: GatewayRoute; meta: Meta };

export type AnalyticsEnvelope = { data: GatewayAnalytics; meta: Meta };

export type ConnectSetup = {
  source: GatewaySource;
  managementConfigured: boolean;
  configurationReadable: boolean;
  status: "healthy" | "degraded" | "down" | "unknown";
  version?: string;
  latencyMs: number | null;
  checkedAt: string;
  message?: string;
  links: ConsoleLinks;
};

export type ConnectSetupEnvelope = { data: ConnectSetup; meta: Meta };

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
  getConnectAnalytics: { method: "GET", path: "/api/v1/connect/analytics" },
  getConnectSummary: { method: "GET", path: "/api/v1/connect/summary" },
  getGatewayMcpServer: { method: "GET", path: "/api/v1/connect/mcp/servers/{resourceId}" },
  getModel: { method: "GET", path: "/api/v1/connect/llm/models/{resourceId}" },
  getOverview: { method: "GET", path: "/api/v1/overview" },
  getProvider: { method: "GET", path: "/api/v1/connect/llm/providers/{resourceId}" },
  getSystemHealth: { method: "GET", path: "/api/v1/system/health" },
  getTrafficRoute: { method: "GET", path: "/api/v1/connect/traffic/routes/{resourceId}" },
  listGatewayMcpServers: { method: "GET", path: "/api/v1/connect/mcp/servers" },
  listModels: { method: "GET", path: "/api/v1/connect/llm/models" },
  listProviders: { method: "GET", path: "/api/v1/connect/llm/providers" },
  listTrafficRoutes: { method: "GET", path: "/api/v1/connect/traffic/routes" },
  streamEvents: { method: "GET", path: "/api/v1/stream" },
  verifyGatewaySetup: { method: "GET", path: "/api/v1/connect/setup" },
} as const;

export interface OperationResponses {
  createAdminSession: undefined;
  getCapabilities: CapabilitiesEnvelope;
  getConnectAnalytics: AnalyticsEnvelope;
  getConnectSummary: ConnectSummaryEnvelope;
  getGatewayMcpServer: MCPEnvelope;
  getModel: ModelEnvelope;
  getOverview: OverviewEnvelope;
  getProvider: ProviderEnvelope;
  getSystemHealth: HealthEnvelope;
  getTrafficRoute: RouteEnvelope;
  listGatewayMcpServers: MCPPageEnvelope;
  listModels: ModelPageEnvelope;
  listProviders: ProviderPageEnvelope;
  listTrafficRoutes: RoutePageEnvelope;
  streamEvents: string;
  verifyGatewaySetup: ConnectSetupEnvelope;
}

export interface OperationBodies {
  createAdminSession: LoginRequest;
}

export type ImplementedOperationId = keyof typeof implementedOperations;
export type OperationResponse<K extends ImplementedOperationId> = OperationResponses[K];
export type OperationBody<K extends keyof OperationBodies> = OperationBodies[K];
