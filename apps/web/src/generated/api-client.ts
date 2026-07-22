// Generated from api/openapi.yaml by scripts/generate-api-client.mjs. Do not edit.

export type Source = "agentgateway" | "agentguard";

export type GatewaySource = "agentgateway";

export type GuardSource = "agentguard";

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

export type TrustResourceType = "tool" | "skill" | "mcp";

export type TrustLabels = {
  boundary: string;
  sensitivity: string;
  integrity: string;
  tags: Array<string>;
};

export type TrustDetection = {
  resourceUpstreamId?: string;
  name?: string;
  label?: string;
  riskLevel: string;
  capabilities: Array<string>;
  riskLabels: Array<string>;
  policyTargets: Array<string>;
  suggestedPlugins: Array<string>;
};

export type TrustResource = {
  id: string;
  upstreamId: string;
  source: GuardSource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  type: TrustResourceType;
  ownerAgentId: string;
  ownerAgentUpstreamId: string;
  sessionId?: string;
  description?: string;
  framework?: string;
  transport?: string;
  remote?: boolean | null;
  toolCount?: number | null;
  labels?: TrustLabels;
  detection?: TrustDetection;
};

export type TrustSession = {
  id: string;
  upstreamId: string;
  source: GuardSource;
  fetchedAt: string;
  rawRef: RawRef;
  agentId: string;
  agentUpstreamId: string;
  userId?: string;
  lastSeen: string | null;
  status: "unknown";
};

export type TrustAgent = {
  id: string;
  upstreamId: string;
  source: GuardSource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  framework: string | null;
  principal: string | null;
  trustLevel: string | null;
  status: "unknown";
  sessions: number;
  tools: number;
  skills: number;
  mcps: number;
  lastActive: string | null;
};

export type TrustAgentWorkspace = {
  agent: TrustAgent;
  sessions: Array<TrustSession>;
  resources: Array<TrustResource>;
};

export type TrustAgentPage = { items: Array<TrustAgent>; nextCursor: string | null; total: number };

export type TrustAgentPageEnvelope = { data: TrustAgentPage; meta: Meta };

export type TrustAgentWorkspaceEnvelope = { data: TrustAgentWorkspace; meta: Meta };

export type TrustResourcePage = {
  items: Array<TrustResource>;
  nextCursor: string | null;
  total: number;
};

export type TrustResourcePageEnvelope = { data: TrustResourcePage; meta: Meta };

export type TrustResourceEnvelope = { data: TrustResource; meta: Meta };

export type TrustScanError = { code: string; message: string; retryable: boolean };

export type TrustScanJob = {
  id: string;
  source: GuardSource;
  agentId: string;
  agentUpstreamId: string;
  resourceType: "skill" | "mcp";
  resourceIds: Array<string>;
  status: "queued" | "running" | "succeeded" | "failed";
  createdAt: string;
  startedAt: string | null;
  completedAt: string | null;
  updatedAt: string;
  results: Array<TrustDetection>;
  warnings: Array<string>;
  error?: TrustScanError;
};

export type TrustScanEnvelope = { data: TrustScanJob; meta: Meta };

export type TrustScanPage = {
  items: Array<TrustScanJob>;
  nextCursor: string | null;
  total: number;
};

export type TrustScanPageEnvelope = { data: TrustScanPage; meta: Meta };

export type LabelUpdate = {
  boundary?: string | null;
  sensitivity?: string | null;
  integrity?: string | null;
  tags?: Array<string>;
};

export type SkillDetectionRequest = { resourceIds: Array<string>; useLlm?: boolean };

export type MCPDetectionRequest = { resourceIds: Array<string> };

export type ProtectPolicy = {
  id: string;
  upstreamId: string;
  source: GatewaySource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  type: "Gateway Policy" | "Content Guardrail";
  scope: string;
  phase: string;
  action: string;
  status: "read-only";
};

export type RuntimeRule = {
  id: string;
  upstreamId: string;
  source: GuardSource;
  fetchedAt: string;
  rawRef: RawRef;
  name: string;
  agentId?: string;
  agentUpstreamId?: string;
  scope: string;
  phase: string;
  action: "ALLOW" | "DENY" | "HUMAN_CHECK" | "LLM_CHECK" | "DEGRADE";
  status: string;
  severity?: string;
  category?: string;
  toolPattern?: string;
  reason?: string;
  userManaged: boolean;
};

export type ProtectPluginPhase = {
  id: string;
  upstreamId: string;
  source: GuardSource;
  fetchedAt: string;
  rawRef: RawRef;
  agentId: string;
  agentUpstreamId: string;
  phase: string;
  configSource: "agent_override" | "server_default" | "none" | "unavailable";
  enabledLocalPlugins: Array<string>;
  enabledRemotePlugins: Array<string>;
  availableLocalPlugins: Array<string>;
  availableRemotePlugins: Array<string>;
};

export type ProtectSnapshot = {
  gatewayPolicies: Array<ProtectPolicy>;
  runtimeRules: Array<RuntimeRule>;
  plugins: Array<ProtectPluginPhase>;
  links: ConsoleLinks;
};

export type ProtectSnapshotEnvelope = { data: ProtectSnapshot; meta: Meta };

export type RuntimeRuleCheckRequest = { source: string };

export type RuleCheckMessage = { message: string };

export type RuntimeRuleCheck = {
  source: GuardSource;
  ok: boolean;
  publishable: boolean;
  ruleCount: number;
  errors: Array<RuleCheckMessage>;
  warnings: Array<RuleCheckMessage>;
  hints: Array<RuleCheckMessage>;
  checkToken?: string;
  expiresAt: string | null;
  requestId: string;
};

export type RuntimeRuleCheckEnvelope = { data: RuntimeRuleCheck; meta: Meta };

export type RuntimeRulePublishRequest = {
  source: string;
  checkToken: string;
  note: string;
  confirmed: boolean;
};

export type ConfirmedActionRequest = { note: string; confirmed: boolean };

export type Approval = {
  id: string;
  upstreamId: string;
  source: GuardSource;
  fetchedAt: string;
  rawRef: RawRef;
  agentId?: string;
  agentUpstreamId?: string;
  sessionId?: string;
  userId?: string;
  eventId?: string;
  eventType: string;
  tool?: string;
  phase: string;
  action: string;
  reason?: string;
  riskScore: number;
  matchedRules: Array<string>;
  status: "pending";
  createdAt: string;
};

export type ApprovalPage = { items: Array<Approval>; nextCursor: string | null; total: number };

export type ApprovalPageEnvelope = { data: ApprovalPage; meta: Meta };

export type ProtectMutationReceipt = {
  operation: "publish-runtime-rule" | "delete-runtime-rule" | "approve-approval" | "deny-approval";
  status: "succeeded";
  source: GuardSource;
  target: string;
  requestId: string;
  completedAt: string;
  message: string;
};

export type ProtectMutationEnvelope = { data: ProtectMutationReceipt; meta: Meta };

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
  rawRef: { source: Source; id: string };
  raw?: { [key: string]: unknown };
};

export type AuditSession = {
  id: string;
  upstreamId: string;
  agentId: string;
  agentUpstreamId: string;
  principal?: string;
  events: number;
  denies: number;
  lastSeen: string | null;
  status: "unknown";
  source: GuardSource;
  rawRef: RawRef;
};

export type AuditData = {
  metrics: Array<Metric>;
  trend: Array<TrendPoint>;
  events: Array<UnifiedEvent>;
  sessions: Array<AuditSession>;
};

export type AuditEnvelope = { data: AuditData; meta: Meta };

export type EventEnvelope = { data: UnifiedEvent; meta: Meta };

export type AuditSessionsEnvelope = { data: Array<AuditSession>; meta: Meta };

export type EventsEnvelope = {
  data: { items: Array<UnifiedEvent>; nextCursor: string | null; total: number };
  meta: Meta;
};

export type ErrorEnvelope = {
  error: { code: string; message: string; source?: Source; requestId: string; retryable: boolean };
};

export const implementedOperations = {
  approveTicket: { method: "POST", path: "/api/v1/protect/approvals/{ticketId}/approve" },
  checkRuntimeRule: { method: "POST", path: "/api/v1/protect/runtime-rules/check" },
  createAdminSession: { method: "POST", path: "/api/v1/auth/session" },
  deleteRuntimeRule: {
    method: "DELETE",
    path: "/api/v1/protect/agents/{agentId}/runtime-rules/{ruleId}",
  },
  denyTicket: { method: "POST", path: "/api/v1/protect/approvals/{ticketId}/deny" },
  detectMcps: { method: "POST", path: "/api/v1/trust/agents/{agentId}/mcps/detect" },
  detectSkills: { method: "POST", path: "/api/v1/trust/agents/{agentId}/skills/detect" },
  getAgent: { method: "GET", path: "/api/v1/trust/agents/{agentId}" },
  getAuditAnalytics: { method: "GET", path: "/api/v1/audit/analytics" },
  getAuditEvent: { method: "GET", path: "/api/v1/audit/events/{source}/{eventId}" },
  getCapabilities: { method: "GET", path: "/api/v1/system/capabilities" },
  getConnectAnalytics: { method: "GET", path: "/api/v1/connect/analytics" },
  getConnectSummary: { method: "GET", path: "/api/v1/connect/summary" },
  getGatewayMcpServer: { method: "GET", path: "/api/v1/connect/mcp/servers/{resourceId}" },
  getModel: { method: "GET", path: "/api/v1/connect/llm/models/{resourceId}" },
  getOverview: { method: "GET", path: "/api/v1/overview" },
  getProvider: { method: "GET", path: "/api/v1/connect/llm/providers/{resourceId}" },
  getSystemHealth: { method: "GET", path: "/api/v1/system/health" },
  getTrafficRoute: { method: "GET", path: "/api/v1/connect/traffic/routes/{resourceId}" },
  getTrustScan: { method: "GET", path: "/api/v1/trust/scans/{scanId}" },
  listAgents: { method: "GET", path: "/api/v1/trust/agents" },
  listApprovals: { method: "GET", path: "/api/v1/protect/approvals" },
  listAuditEvents: { method: "GET", path: "/api/v1/audit/events" },
  listAuditSessions: { method: "GET", path: "/api/v1/audit/sessions" },
  listGatewayMcpServers: { method: "GET", path: "/api/v1/connect/mcp/servers" },
  listModels: { method: "GET", path: "/api/v1/connect/llm/models" },
  listPolicies: { method: "GET", path: "/api/v1/protect/policies" },
  listProviders: { method: "GET", path: "/api/v1/connect/llm/providers" },
  listTrafficRoutes: { method: "GET", path: "/api/v1/connect/traffic/routes" },
  listTrustResources: { method: "GET", path: "/api/v1/trust/resources" },
  listTrustScans: { method: "GET", path: "/api/v1/trust/scans" },
  publishRuntimeRule: { method: "POST", path: "/api/v1/protect/agents/{agentId}/runtime-rules" },
  streamEvents: { method: "GET", path: "/api/v1/stream" },
  updateToolLabels: { method: "PATCH", path: "/api/v1/trust/agents/{agentId}/tools/{tool}/labels" },
  verifyGatewaySetup: { method: "GET", path: "/api/v1/connect/setup" },
} as const;

export interface OperationResponses {
  approveTicket: ProtectMutationEnvelope;
  checkRuntimeRule: RuntimeRuleCheckEnvelope;
  createAdminSession: undefined;
  deleteRuntimeRule: ProtectMutationEnvelope;
  denyTicket: ProtectMutationEnvelope;
  detectMcps: TrustScanEnvelope;
  detectSkills: TrustScanEnvelope;
  getAgent: TrustAgentWorkspaceEnvelope;
  getAuditAnalytics: AuditEnvelope;
  getAuditEvent: EventEnvelope;
  getCapabilities: CapabilitiesEnvelope;
  getConnectAnalytics: AnalyticsEnvelope;
  getConnectSummary: ConnectSummaryEnvelope;
  getGatewayMcpServer: MCPEnvelope;
  getModel: ModelEnvelope;
  getOverview: OverviewEnvelope;
  getProvider: ProviderEnvelope;
  getSystemHealth: HealthEnvelope;
  getTrafficRoute: RouteEnvelope;
  getTrustScan: TrustScanEnvelope;
  listAgents: TrustAgentPageEnvelope;
  listApprovals: ApprovalPageEnvelope;
  listAuditEvents: EventsEnvelope;
  listAuditSessions: AuditSessionsEnvelope;
  listGatewayMcpServers: MCPPageEnvelope;
  listModels: ModelPageEnvelope;
  listPolicies: ProtectSnapshotEnvelope;
  listProviders: ProviderPageEnvelope;
  listTrafficRoutes: RoutePageEnvelope;
  listTrustResources: TrustResourcePageEnvelope;
  listTrustScans: TrustScanPageEnvelope;
  publishRuntimeRule: ProtectMutationEnvelope;
  streamEvents: string;
  updateToolLabels: TrustResourceEnvelope;
  verifyGatewaySetup: ConnectSetupEnvelope;
}

export interface OperationBodies {
  approveTicket: ConfirmedActionRequest;
  checkRuntimeRule: RuntimeRuleCheckRequest;
  createAdminSession: LoginRequest;
  deleteRuntimeRule: ConfirmedActionRequest;
  denyTicket: ConfirmedActionRequest;
  detectMcps: MCPDetectionRequest;
  detectSkills: SkillDetectionRequest;
  publishRuntimeRule: RuntimeRulePublishRequest;
  updateToolLabels: LabelUpdate;
}

export type ImplementedOperationId = keyof typeof implementedOperations;
export type OperationResponse<K extends ImplementedOperationId> = OperationResponses[K];
export type OperationBody<K extends keyof OperationBodies> = OperationBodies[K];
