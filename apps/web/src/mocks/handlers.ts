import { delay, http, HttpResponse } from "msw";

import type {
  ApiFailure,
  AuditData,
  Envelope,
  OverviewData,
  ResponseMeta,
  Scenario,
  Source,
  UnifiedEvent,
} from "../types";
import type {
  ConfirmedActionRequest,
  DemoCreateRunRequest,
  DemoRun,
  DemoScenario,
  DemoStatusComponent,
  DiagnosticsData,
  GatewayPolicyConfiguration,
  GatewayPolicyDeleteRequest,
  GatewayPolicyMutationRequest,
  GatewayPolicySetting,
  LabelUpdate,
  LlmConfiguration,
  LlmCredentialState,
  LlmDeleteRequest,
  LlmModelDraft,
  LlmModelMutationRequest,
  LlmModelSetting,
  LlmProviderDraft,
  LlmProviderDeleteRequest,
  LlmProviderMutationRequest,
  LlmProviderSetting,
  McpConfiguration,
  McpDeleteRequest,
  McpServerDraft,
  McpServerMutationRequest,
  McpServerSetting,
  McpSettingsMutationRequest,
  MCPDetectionRequest,
  ProtectSnapshot,
  RuntimeRule,
  RuntimeRuleCheckRequest,
  RuntimeRulePublishRequest,
  SkillDetectionRequest,
  TraceSpan,
  TraceSummary,
  TrafficBindMutationRequest,
  TrafficConfiguration,
  TrafficDeleteRequest,
  TrafficListenerMutationRequest,
  TrafficListenerSetting,
  TrafficRouteMutationRequest,
  TrafficRouteSetting,
  TrustResource,
  TrustScanJob,
} from "../generated/api-client";
import {
  auditData,
  baseEvents,
  connectData,
  demoRuns,
  demoScenarioDefinitions,
  demoStatus,
  mockTraceDetails,
  mockTraceSpanDetails,
  mockTraceSummaries,
  overviewData,
  protectApprovals,
  protectSnapshot,
  trustAgents,
  trustResources,
  trustScans,
} from "./data";

const capturedAt = "2026-07-21T12:42:10Z";

const capabilityData = [
  {
    id: "gateway.runtime",
    source: "agentgateway" as const,
    status: "supported" as const,
    checkedAt: capturedAt,
    reason: "Mock live /api/runtime probe succeeded",
  },
  {
    id: "gateway.configuration",
    source: "agentgateway" as const,
    status: "supported" as const,
    checkedAt: capturedAt,
    reason: "Mock configuration probes succeeded",
  },
  {
    id: "gateway.admin-auth",
    source: "agentgateway" as const,
    status: "unavailable" as const,
    checkedAt: capturedAt,
    reason: "Pinned upstream does not expose native admin authentication",
  },
  {
    id: "guard.resources",
    source: "agentguard" as const,
    status: "supported" as const,
    checkedAt: capturedAt,
    reason: "Mock protected resource probes succeeded",
  },
  {
    id: "guard.approvals",
    source: "agentguard" as const,
    status: "supported" as const,
    checkedAt: capturedAt,
    reason: "Mock protected approval probe succeeded",
  },
];

function scenarioFrom(request: Request): Scenario {
  const value = new URL(request.url).searchParams.get("scenario");
  if (["empty", "loading", "partial", "error"].includes(value ?? "")) {
    return value as Scenario;
  }
  return "normal";
}

function failure(source?: Source): Response {
  return HttpResponse.json(
    {
      error: {
        code: "UPSTREAM_UNAVAILABLE",
        message: source
          ? `${source} is unavailable in this demo state`
          : "All sources are unavailable",
        source,
        requestId: "req_mock_outage_001",
        retryable: true,
      },
    },
    { status: 503 },
  );
}

function meta(source?: Source, partial = false): ResponseMeta {
  return {
    source,
    sourceVersion:
      source === "agentgateway" ? "v1.3.1" : source === "agentguard" ? "v2.1" : undefined,
    fetchedAt: capturedAt,
    stale: false,
    partial,
    sourceFailures: partial
      ? [
          {
            source: "agentguard",
            code: "UPSTREAM_TIMEOUT",
            message: "AgentGuard mock probe exceeded the 2s source timeout.",
          },
        ]
      : undefined,
  };
}

async function respond<T>(
  request: Request,
  data: T,
  emptyData: T,
  source?: Source,
): Promise<Response> {
  const scenario = scenarioFrom(request);
  if (scenario === "loading") {
    await delay(30_000);
  }
  if (scenario === "error") {
    return failure();
  }
  if (scenario === "empty") {
    return HttpResponse.json({ data: emptyData, meta: meta(source) } satisfies Envelope<T>);
  }
  return HttpResponse.json({
    data,
    meta: meta(source, scenario === "partial"),
  } satisfies Envelope<T>);
}

const emptyOverview: OverviewData = {
  ...overviewData,
  health: overviewData.health,
  metrics: overviewData.metrics.map((metric) => ({ ...metric, value: 0, delta: 0, trend: "flat" })),
  trend: [],
  events: [],
  setup: {
    complete: false,
    steps: [
      { id: "gateway", label: "Connect agentgateway", complete: false },
      { id: "guard", label: "Connect AgentGuard", complete: false },
      {
        id: "verify",
        label: "Send a verification request",
        complete: false,
        command: "curl http://localhost:8080/api/v1/system/health",
      },
    ],
  },
};

const emptyConnectSummary = {
  ...connectData.summary,
  counts: connectData.summary.counts.map((item) => ({ ...item, value: 0 })),
};

const emptyProtect: ProtectSnapshot = {
  gatewayPolicies: [],
  runtimeRules: [],
  plugins: [],
  links: protectSnapshot.links,
};

const emptyAudit: AuditData = {
  metrics: auditData.metrics.map((metric) => ({ ...metric, value: 0, delta: 0, trend: "flat" })),
  trend: [],
  events: [],
  sessions: [],
  links: auditData.links,
};

function completeAuditEvent(event: UnifiedEvent): UnifiedEvent {
  if (event.id === "gateway-log-73b1") {
    return {
      ...event,
      raw: {
        id: "log-73b1",
        startedAt: "2026-07-21T12:41:31.680Z",
        completedAt: "2026-07-21T12:41:32Z",
        durationMs: 320,
        traceId: "trace-mock-73b1",
        spanId: "span-mock-73b1",
        httpStatus: 200,
        error: null,
        genAi: {
          operationName: "chat",
          providerName: "openai-compatible",
          requestModel: "chat-primary-v1",
          responseModel: "chat-primary-v1",
        },
        usage: { inputTokens: 18, outputTokens: 9, totalTokens: 27 },
        cost: 0.00027,
        hasPayload: true,
        attributes: {
          "agentgateway.user": "administrator-visible-user",
          "request.authorization": "Bearer synthetic-mock-value",
          "fixture.nested": { retained: true },
        },
        payload: {
          requestPrompt: [
            { role: "system", content: "Answer with complete source context." },
            { role: "user", content: "Show the full retained request." },
          ],
          responseCompletion: {
            choices: [
              {
                message: {
                  role: "assistant",
                  content: "This is the complete retained completion.",
                  tool_calls: [
                    {
                      function: {
                        name: "lookup_context",
                        arguments: { scope: "complete" },
                      },
                    },
                  ],
                },
              },
            ],
          },
        },
      },
    };
  }
  if (event.source === "agentguard") {
    return {
      ...event,
      raw: {
        event: {
          event_id: event.rawRef.id,
          event_type: event.kind,
          tool_call: {
            tool_name: event.target?.tool,
            args: { complete: true, sourceEvent: event.rawRef.id },
            result: { retained: true },
          },
        },
        decision: {
          action: event.decision,
          reason: "Complete mock decision reason",
          plugin_result: { retained: true },
        },
        runtime_state: { retained: true },
      },
    };
  }
  return { ...event, raw: { id: event.rawRef.id, hasPayload: false } };
}

function listResponse<T>(request: Request, data: T[], source: Source) {
  return respond(request, data, [], source);
}

async function pageResponse<T>(request: Request, data: T[], source: Source) {
  const url = new URL(request.url);
  const search = (url.searchParams.get("q") ?? "").toLowerCase();
  const offset = Number(url.searchParams.get("cursor") ?? "0") || 0;
  const limit = Number(url.searchParams.get("limit") ?? "25") || 25;
  const filtered = search
    ? data.filter((item) => JSON.stringify(item).toLowerCase().includes(search))
    : data;
  const items = filtered.slice(offset, offset + limit);
  const nextCursor = offset + limit < filtered.length ? String(offset + limit) : null;
  return respond(
    request,
    { items, nextCursor, total: filtered.length },
    { items: [], nextCursor: null, total: 0 },
    source,
  );
}

let mockTrustResources = trustResources.map((resource) => structuredClone(resource));
let mockProtectSnapshot = structuredClone(protectSnapshot);
let mockProtectApprovals = structuredClone(protectApprovals);
let mockDemoRuns = structuredClone(demoRuns);
const mockDemoRequests = new Map<string, string>();
let mockGatewayPolicyRevision = 1;

const activeDemoStatuses = new Set(["queued", "starting", "running", "waiting_approval"]);

function mockDemoActiveRun(): DemoRun | undefined {
  return mockDemoRuns.find((run) => activeDemoStatuses.has(run.status));
}

function demoResponse(run: DemoRun, status = 200): Response {
  return HttpResponse.json({ data: run, meta: meta() }, { status });
}

function demoFailure(
  status: number,
  code: string,
  message: string,
  retryable = false,
  failedChecks?: Array<DemoStatusComponent>,
): Response {
  return HttpResponse.json(
    {
      error: {
        code,
        message,
        requestId: `req_mock_demo_${status}`,
        retryable,
        ...(failedChecks?.length ? { failedChecks } : {}),
      },
    } satisfies ApiFailure,
    { status },
  );
}

function resetMockDemoRun(scenario: DemoScenario): DemoRun {
  const template = structuredClone(demoRuns.find((run) => run.scenario === scenario)!);
  const now = new Date();
  template.requestedAt = now.toISOString();
  template.startedAt = new Date(now.getTime() + 100).toISOString();
  template.lastHeartbeatAt = new Date(now.getTime() + 500).toISOString();
  template.runVersion += 1;
  if (scenario === "approval") {
    template.status = "waiting_approval";
    template.outcome = "none";
    template.completedAt = null;
    template.currentStep = "guarded_action";
    template.completedSteps = 7;
    template.approval = {
      ...template.approval!,
      ticketId: "ticket-demo-active",
      upstreamId: "ticket-upstream-demo-active",
      fetchedAt: new Date(now.getTime() + 600).toISOString(),
      rawRef: { source: "agentguard", id: "/v1/backend/approvals/0" },
      status: "pending",
      createdAt: new Date(now.getTime() + 500).toISOString(),
    };
    template.links.approval = "/protect/approvals?ticketId=ticket-demo-active";
    if (!mockProtectApprovals.some((approval) => approval.id === template.approval!.ticketId)) {
      mockProtectApprovals.unshift({
        id: template.approval.ticketId,
        upstreamId: template.approval.upstreamId,
        source: "agentguard",
        fetchedAt: template.approval.fetchedAt,
        rawRef: template.approval.rawRef,
        agentId: template.approval.agentId,
        agentUpstreamId: template.approval.agentUpstreamId,
        sessionId: template.approval.sessionId,
        eventId: template.approval.eventId,
        eventType: template.approval.eventType,
        tool: template.approval.tool,
        phase: template.approval.phase,
        action: template.approval.action,
        reason: template.approval.reason,
        riskScore: template.approval.riskScore,
        matchedRules: template.approval.matchedRules,
        status: "pending",
        createdAt: template.approval.createdAt,
      });
    }
  }
  mockDemoRuns = [template, ...mockDemoRuns.filter((run) => run.runId !== template.runId)];
  return template;
}

function resolveMockDemoApproval(ticketId: string, decision: "approve" | "deny") {
  const run = mockDemoRuns.find((item) => item.approval?.ticketId === ticketId);
  if (!run?.approval) return;
  const completedAt = new Date().toISOString();
  run.approval.status = decision === "approve" ? "approved" : "denied";
  run.status = "succeeded";
  run.outcome = decision === "approve" ? "approved" : "denied";
  run.currentStep = "finish";
  run.completedSteps = run.totalSteps;
  run.completedAt = completedAt;
  run.lastHeartbeatAt = completedAt;
  run.runVersion += 1;
}

type MockPolicyCatalogEntry = {
  key: string;
  title: string;
  group: string;
  description: string;
  phase: string;
  action: string;
  value?: unknown;
};

const mockLlmPolicyCatalog: MockPolicyCatalogEntry[] = [
  {
    key: "cors",
    title: "CORS",
    group: "Access",
    description: "Handle CORS requests and response headers.",
    phase: "Request",
    action: "Authorize",
    value: { allowOrigins: ["https://console.example"] },
  },
  {
    key: "apiKey",
    title: "API keys",
    group: "Access",
    description: "Authenticate incoming requests with API keys.",
    phase: "Request",
    action: "Authenticate",
  },
  {
    key: "basicAuth",
    title: "Basic auth",
    group: "Access",
    description: "Authenticate incoming requests with Basic Auth.",
    phase: "Request",
    action: "Authenticate",
  },
  {
    key: "jwtAuth",
    title: "JWT auth",
    group: "Access",
    description: "Authenticate incoming requests with JWT bearer tokens.",
    phase: "Request",
    action: "Authenticate",
  },
  {
    key: "oidc",
    title: "OIDC",
    group: "Access",
    description: "Authenticate browser requests with OIDC.",
    phase: "Request",
    action: "Authenticate",
  },
  {
    key: "authorization",
    title: "Authorization",
    group: "Access",
    description: "Apply authorization rules to incoming requests.",
    phase: "Request",
    action: "Authorize",
  },
  {
    key: "extAuthz",
    title: "External authorization",
    group: "Access",
    description: "Call an external authorization service.",
    phase: "Request",
    action: "Authorize",
  },
  {
    key: "guardrails",
    title: "Guardrails",
    group: "Safety",
    description: "Apply prompt and response guardrails.",
    phase: "Request + Response",
    action: "Inspect",
    value: {
      streaming: "Enabled",
      request: [
        {
          regex: {
            action: "mask",
            rules: [{ builtin: "email" }, { pattern: "(?i)secret-[a-z0-9]+" }],
          },
          rejection: {
            status: 422,
            body: "Request rejected by guardrail",
            headers: { set: { "x-guardrail-result": "rejected" } },
          },
        },
        {
          webhook: {
            target: { service: { name: "default/guardrail", port: 8080 } },
            failureMode: "failOpen",
            forwardHeaderMatches: [{ name: "x-tenant-id", value: { regex: ".+" } }],
          },
        },
      ],
      response: [
        {
          azureContentSafety: {
            endpoint: "safety.example.invalid",
            analyzeText: { severityThreshold: 2, blocklistNames: ["restricted"] },
          },
        },
      ],
    },
  },
  {
    key: "localRateLimit",
    title: "Local rate limit",
    group: "Traffic Shaping",
    description: "Apply local rate limits.",
    phase: "Request",
    action: "Limit",
    value: [{ requests: 120, unit: "Minute" }],
  },
  {
    key: "remoteRateLimit",
    title: "Remote rate limit",
    group: "Traffic Shaping",
    description: "Run remote rate-limit checks.",
    phase: "Request",
    action: "Limit",
  },
  {
    key: "transformations",
    title: "Transformations",
    group: "Mutation",
    description: "Modify requests and responses.",
    phase: "Request + Response",
    action: "Transform",
  },
  {
    key: "extProc",
    title: "External processor",
    group: "Mutation",
    description: "Call an external processing service.",
    phase: "Request + Response",
    action: "Process",
  },
];

const mockModelPolicyCatalog: MockPolicyCatalogEntry[] = [
  {
    key: "authorization",
    title: "Authorization",
    group: "Access",
    description: "Authorize requests for this direct model.",
    phase: "Request",
    action: "Authorize",
  },
  {
    key: "defaults",
    title: "Request defaults",
    group: "Request Mutation",
    description: "Set default request-body values.",
    phase: "Request",
    action: "Transform",
    value: { temperature: 0.2 },
  },
  {
    key: "overrides",
    title: "Request overrides",
    group: "Request Mutation",
    description: "Replace request-body values.",
    phase: "Request",
    action: "Transform",
  },
  {
    key: "transformation",
    title: "Request transformation",
    group: "Request Mutation",
    description: "Compute request values with CEL.",
    phase: "Request",
    action: "Transform",
  },
  {
    key: "requestHeaders",
    title: "Request headers",
    group: "Request Mutation",
    description: "Modify provider request headers.",
    phase: "Request",
    action: "Transform",
    value: { set: { "x-model-tier": "fast" } },
  },
  {
    key: "responseHeaders",
    title: "Response headers",
    group: "Response Mutation",
    description: "Modify model response headers.",
    phase: "Response",
    action: "Transform",
  },
  {
    key: "tls",
    title: "Backend TLS",
    group: "Backend",
    description: "Configure TLS for the model backend.",
    phase: "Backend",
    action: "Secure",
  },
  {
    key: "backendTLS",
    title: "Backend TLS compatibility alias",
    group: "Backend",
    description: "Configure the verified TLS compatibility alias.",
    phase: "Backend",
    action: "Secure",
  },
  {
    key: "auth",
    title: "Backend authentication",
    group: "Backend",
    description: "Configure backend authentication.",
    phase: "Backend",
    action: "Authenticate",
  },
  {
    key: "health",
    title: "Backend health",
    group: "Backend",
    description: "Configure backend outlier detection.",
    phase: "Backend",
    action: "Monitor",
  },
  {
    key: "backendTunnel",
    title: "Backend tunnel",
    group: "Backend",
    description: "Configure a backend tunnel.",
    phase: "Backend",
    action: "Tunnel",
  },
  {
    key: "guardrails",
    title: "Guardrails",
    group: "Safety",
    description: "Apply direct-model guardrails.",
    phase: "Request + Response",
    action: "Inspect",
  },
  {
    key: "promptCaching",
    title: "Prompt caching",
    group: "Caching",
    description: "Configure prompt cache points.",
    phase: "Request",
    action: "Cache",
    value: { minTokens: 1024 },
  },
];

const mockMcpPolicyCatalog: MockPolicyCatalogEntry[] = [
  {
    key: "mcpAuthentication",
    title: "MCP authentication",
    group: "MCP",
    description: "Authenticate MCP clients.",
    phase: "Request",
    action: "Authenticate",
  },
  {
    key: "mcpAuthorization",
    title: "MCP authorization",
    group: "MCP",
    description: "Authorize MCP requests.",
    phase: "Request",
    action: "Authorize",
    value: { rules: [{ action: "allow", resource: "tools/*" }] },
  },
  {
    key: "mcpGuardrails",
    title: "MCP guardrails",
    group: "MCP",
    description: "Apply processors to MCP traffic.",
    phase: "Request + Response",
    action: "Inspect",
    value: {
      processors: [
        {
          kind: "remote",
          backend: "guardrail-backend",
          failureMode: "failOpen",
          methods: {
            "tools/call": "full",
            "tools/*": "request",
            "*/list": "response",
            "*": "off",
          },
          metadata: { tenant: "jwt.sub" },
          requestHeaders: {
            allowed: ["authorization", "x-tenant-id"],
            disallowed: ["cookie"],
          },
          policies: {
            requestHeaderModifier: { set: { "x-policy-source": "mcp" } },
          },
        },
      ],
    },
  },
  {
    key: "authorization",
    title: "Authorization",
    group: "Access",
    description: "Apply HTTP authorization rules.",
    phase: "Request",
    action: "Authorize",
  },
  {
    key: "cors",
    title: "CORS",
    group: "Access",
    description: "Handle MCP CORS requests.",
    phase: "Request",
    action: "Authorize",
    value: { allowOrigins: ["https://console.example"] },
  },
  {
    key: "extAuthz",
    title: "External authorization",
    group: "Access",
    description: "Call an external authorization service.",
    phase: "Request",
    action: "Authorize",
  },
  {
    key: "jwtAuth",
    title: "JWT auth",
    group: "Access",
    description: "Authenticate with JWT bearer tokens.",
    phase: "Request",
    action: "Authenticate",
  },
  {
    key: "localRateLimit",
    title: "Local rate limit",
    group: "Traffic Shaping",
    description: "Apply local MCP rate limits.",
    phase: "Request",
    action: "Limit",
  },
  {
    key: "remoteRateLimit",
    title: "Remote rate limit",
    group: "Traffic Shaping",
    description: "Run remote MCP rate-limit checks.",
    phase: "Request",
    action: "Limit",
  },
  {
    key: "transformations",
    title: "Transformations",
    group: "Mutation",
    description: "Modify MCP requests and responses.",
    phase: "Request + Response",
    action: "Transform",
  },
  {
    key: "extProc",
    title: "External processor",
    group: "Mutation",
    description: "Call an external MCP processor.",
    phase: "Request + Response",
    action: "Process",
  },
];

function mockPolicySettings(
  family: GatewayPolicySetting["family"],
  target: string,
  scope: string,
  basePath: string,
  catalog: MockPolicyCatalogEntry[],
): GatewayPolicySetting[] {
  return catalog.map((entry) => {
    const rawPath = `${basePath}/${entry.key}`;
    return {
      id: `mock-${family}-${target.toLowerCase().replaceAll(/[^a-z0-9]+/g, "-")}-${entry.key}`,
      upstreamId: rawPath,
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: rawPath },
      family,
      target,
      key: entry.key,
      title: entry.title,
      group: entry.group,
      description: entry.description,
      scope,
      phase: entry.phase,
      action: entry.action,
      enabled: entry.value !== undefined,
      editable: true,
      value: entry.value === undefined ? null : structuredClone(entry.value),
    };
  });
}

let mockGatewayPolicyConfiguration: GatewayPolicyConfiguration = {
  source: "agentgateway",
  fetchedAt: capturedAt,
  revisionToken: "mock-gateway-policy-revision-1",
  settings: [
    ...mockPolicySettings("llm", "LLM Gateway", "Gateway", "/llm/policies", mockLlmPolicyCatalog),
    ...mockPolicySettings("model", "fast", "Model", "/llm/models/1", mockModelPolicyCatalog),
    ...mockPolicySettings("mcp", "MCP Gateway", "Gateway", "/mcp/policies", mockMcpPolicyCatalog),
  ],
  links: connectData.summary.links,
};
let mockLlmRevision = 1;
let nextLlmResource = 100;
let mockLlmConfiguration: LlmConfiguration = {
  source: "agentgateway",
  fetchedAt: capturedAt,
  revisionToken: "mock-llm-revision-1",
  providers: [
    {
      id: "openai-shared",
      upstreamId: "openai-shared",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/llm/providers/0" },
      name: "openai-shared",
      providerType: "openai",
      params: { model: "gpt-5.4-nano" },
      formats: [],
      credential: { configured: true, kind: "environment" },
      modelCount: 1,
      editable: true,
    },
  ],
  models: [
    {
      id: "openai-wildcard",
      upstreamId: "openai/*",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/llm/models/0" },
      name: "openai/*",
      providerMode: "builtin",
      providerType: "openai",
      params: {},
      formats: [],
      visibility: "public",
      upstreamMode: "incoming",
      credential: { configured: false, kind: "ambient" },
      editable: true,
    },
    {
      id: "fast",
      upstreamId: "fast",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/llm/models/1" },
      name: "fast",
      providerMode: "reference",
      providerReference: "openai-shared",
      params: { model: "gpt-5.4-nano" },
      formats: [],
      visibility: "public",
      upstreamMode: "explicit",
      credential: { configured: false, kind: "ambient" },
      editable: true,
    },
  ],
  virtualModels: connectData.models.filter((model) => model.kind === "virtual"),
  links: connectData.summary.links,
};
let mockMcpRevision = 1;
let nextMcpResource = 100;
let mockMcpConfiguration: McpConfiguration = {
  source: "agentgateway",
  fetchedAt: capturedAt,
  revisionToken: "mock-mcp-revision-1",
  settings: {
    port: 3000,
    statefulMode: "stateful",
    prefixMode: "conditional",
    failureMode: "failClosed",
    hasPolicies: false,
  },
  servers: [
    {
      id: "everything",
      upstreamId: "everything",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/mcp/targets/0" },
      name: "everything",
      transport: "mcp",
      scope: "gateway",
      network: { mode: "url", host: "http://localhost:3001/mcp" },
      hasPolicies: false,
      editable: true,
    },
    {
      id: "filesystem",
      upstreamId: "filesystem",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/mcp/targets/1" },
      name: "filesystem",
      transport: "stdio",
      scope: "gateway",
      stdio: {
        command: "npx",
        arguments: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"],
        environment: { LOG_LEVEL: "info" },
        clearEnvironment: false,
      },
      hasPolicies: true,
      editable: true,
    },
    {
      id: "catalog-api",
      upstreamId: "catalog-api",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/mcp/targets/2" },
      name: "catalog-api",
      transport: "openapi",
      scope: "gateway",
      network: { mode: "url", host: "https://catalog.example/openapi" },
      hasPolicies: false,
      editable: false,
    },
  ],
  inlineServers: [],
  links: connectData.summary.links,
};
let mockTrafficRevision = 1;
let nextTrafficResource = 100;
let mockTrafficConfiguration: TrafficConfiguration = {
  source: "agentgateway",
  fetchedAt: capturedAt,
  revisionToken: "mock-traffic-revision-1",
  binds: [
    {
      id: "bind-8080",
      upstreamId: "8080",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/binds/0" },
      port: 8080,
      tunnelProtocol: "direct",
      listenerCount: 1,
      routeCount: 1,
      backendCount: 2,
    },
    {
      id: "bind-9090",
      upstreamId: "9090",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/binds/1" },
      port: 9090,
      tunnelProtocol: "direct",
      listenerCount: 1,
      routeCount: 1,
      backendCount: 1,
    },
  ],
  listeners: [
    {
      id: "listener-public",
      upstreamId: "public-http",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/binds/0/listeners/0" },
      bindId: "bind-8080",
      port: 8080,
      name: "public-http",
      hostname: "example.com",
      protocol: "HTTPS",
      routeCount: 1,
      backendCount: 2,
      configuration: {
        name: "public-http",
        hostname: "example.com",
        protocol: "HTTPS",
        tls: { cert: "/etc/certs/tls.crt", key: "/etc/certs/tls.key" },
        policies: { cors: { allowOrigins: ["https://console.example"] } },
      },
    },
    {
      id: "listener-tcp",
      upstreamId: "database",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/binds/1/listeners/0" },
      bindId: "bind-9090",
      port: 9090,
      name: "database",
      hostname: "db.example.com",
      protocol: "TCP",
      routeCount: 1,
      backendCount: 1,
      configuration: { name: "database", hostname: "db.example.com", protocol: "TCP" },
    },
  ],
  routes: [
    {
      id: "route-api",
      upstreamId: "api",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/binds/0/listeners/0/routes/0" },
      listenerId: "listener-public",
      listener: "public-http",
      port: 8080,
      kind: "http",
      name: "api",
      hostnames: ["example.com"],
      backendCount: 2,
      configuration: {
        name: "api",
        hostnames: ["example.com"],
        matches: [
          {
            path: { pathPrefix: "/api" },
            method: "POST",
            headers: [{ name: "x-tenant", value: { exact: "admin" } }],
            query: [{ name: "debug", value: { regex: "true|1" } }],
          },
        ],
        policies: { timeout: { requestTimeout: "30s" } },
        backends: [
          { host: "localhost:9000", weight: 2, policies: { backendAuth: { token: "full-value" } } },
          { dynamic: {} },
        ],
      },
    },
    {
      id: "route-mysql",
      upstreamId: "mysql",
      source: "agentgateway",
      fetchedAt: capturedAt,
      rawRef: { source: "agentgateway", id: "/binds/1/listeners/0/tcpRoutes/0" },
      listenerId: "listener-tcp",
      listener: "database",
      port: 9090,
      kind: "tcp",
      name: "mysql",
      hostnames: ["db.example.com"],
      backendCount: 1,
      configuration: {
        name: "mysql",
        hostnames: ["db.example.com"],
        backends: [{ service: { name: "default/mysql", port: 3306 } }],
      },
    },
  ],
  links: connectData.summary.links,
};
const ruleChecks = new Map<string, string>();
const approvalAttempts = new Map<string, number>();

function protectFailure(
  status: number,
  code: string,
  message: string,
  retryable = false,
): Response {
  return HttpResponse.json(
    {
      error: {
        code,
        message,
        source: "agentguard",
        requestId: `req_mock_protect_${status}`,
        retryable,
      },
    } satisfies ApiFailure,
    { status },
  );
}

function protectReceipt(operation: string, target: string, message: string) {
  const completedAt = new Date().toISOString();
  return HttpResponse.json({
    data: {
      operation,
      status: "succeeded",
      source: "agentguard",
      target,
      requestId: `req_mock_${operation.replaceAll("-", "_")}`,
      completedAt,
      message,
    },
    meta: { ...meta("agentguard"), fetchedAt: completedAt },
  });
}

function acceptGatewayPolicyRevision(revisionToken: string): Response | undefined {
  if (revisionToken !== mockGatewayPolicyConfiguration.revisionToken) {
    return llmFailure(
      409,
      "CONFIGURATION_CHANGED",
      "agentgateway configuration changed. Refresh and retry the operation.",
    );
  }
  return undefined;
}

function gatewayPolicyReceipt(operation: string, target: string, message: string) {
  mockGatewayPolicyRevision += 1;
  const completedAt = new Date().toISOString();
  mockGatewayPolicyConfiguration.revisionToken = `mock-gateway-policy-revision-${mockGatewayPolicyRevision}`;
  mockGatewayPolicyConfiguration.fetchedAt = completedAt;
  mockMcpConfiguration.settings.hasPolicies = mockGatewayPolicyConfiguration.settings.some(
    (setting) => setting.family === "mcp" && setting.enabled,
  );
  return HttpResponse.json({
    data: {
      operation,
      status: "succeeded",
      source: "agentgateway",
      target,
      requestId: `req_mock_${operation.replaceAll("-", "_")}`,
      completedAt,
      message,
    },
    meta: { ...meta("agentgateway"), fetchedAt: completedAt },
  });
}

function llmFailure(status: number, code: string, message: string): Response {
  return HttpResponse.json(
    {
      error: {
        code,
        message,
        source: "agentgateway",
        requestId: `req_mock_llm_${status}`,
        retryable: status >= 500,
      },
    } satisfies ApiFailure,
    { status },
  );
}

function acceptLlmRevision(revisionToken: string): Response | undefined {
  if (revisionToken !== mockLlmConfiguration.revisionToken) {
    return llmFailure(
      409,
      "CONFIGURATION_CHANGED",
      "agentgateway configuration changed. Refresh and retry the operation.",
    );
  }
  return undefined;
}

function nextLlmRevision() {
  mockLlmRevision += 1;
  mockLlmConfiguration.revisionToken = `mock-llm-revision-${mockLlmRevision}`;
  mockLlmConfiguration.fetchedAt = new Date().toISOString();
  for (const provider of mockLlmConfiguration.providers) {
    provider.modelCount = mockLlmConfiguration.models.filter(
      (model) => model.providerMode === "reference" && model.providerReference === provider.name,
    ).length;
  }
}

function acceptMcpRevision(revisionToken: string): Response | undefined {
  if (revisionToken !== mockMcpConfiguration.revisionToken) {
    return llmFailure(
      409,
      "CONFIGURATION_CHANGED",
      "agentgateway configuration changed. Refresh and retry the operation.",
    );
  }
  return undefined;
}

function nextMcpRevision() {
  mockMcpRevision += 1;
  mockMcpConfiguration.revisionToken = `mock-mcp-revision-${mockMcpRevision}`;
  mockMcpConfiguration.fetchedAt = new Date().toISOString();
  const count = connectData.summary.counts.find((item) => item.id === "mcp-targets");
  if (count) count.value = mockMcpConfiguration.servers.length;
}

function mcpServerSetting(draft: McpServerDraft, current?: McpServerSetting): McpServerSetting {
  const index = current
    ? mockMcpConfiguration.servers.findIndex((server) => server.id === current.id)
    : mockMcpConfiguration.servers.length;
  return {
    id: current?.id ?? `mock-mcp-${nextMcpResource++}`,
    upstreamId: draft.name,
    source: "agentgateway",
    fetchedAt: new Date().toISOString(),
    rawRef: { source: "agentgateway", id: `/mcp/targets/${Math.max(index, 0)}` },
    name: draft.name,
    transport: draft.transport,
    scope: "gateway",
    network: draft.network ? structuredClone(draft.network) : undefined,
    stdio: draft.stdio ? structuredClone(draft.stdio) : undefined,
    hasPolicies: current?.hasPolicies ?? false,
    editable: true,
  };
}

function mcpReceipt(operation: string, target: string, message: string) {
  nextMcpRevision();
  const completedAt = new Date().toISOString();
  return HttpResponse.json({
    data: {
      operation,
      status: "succeeded",
      source: "agentgateway",
      target,
      requestId: `req_mock_${operation.replaceAll("-", "_")}`,
      completedAt,
      message,
    },
    meta: { ...meta("agentgateway"), fetchedAt: completedAt },
  });
}

function acceptTrafficRevision(revisionToken: string): Response | undefined {
  if (revisionToken !== mockTrafficConfiguration.revisionToken) {
    return llmFailure(
      409,
      "CONFIGURATION_CHANGED",
      "agentgateway configuration changed. Refresh and retry the operation.",
    );
  }
  return undefined;
}

function nextTrafficRevision() {
  mockTrafficRevision += 1;
  const fetchedAt = new Date().toISOString();
  mockTrafficConfiguration.revisionToken = `mock-traffic-revision-${mockTrafficRevision}`;
  mockTrafficConfiguration.fetchedAt = fetchedAt;
  for (const listener of mockTrafficConfiguration.listeners) {
    const routes = mockTrafficConfiguration.routes.filter(
      (route) => route.listenerId === listener.id,
    );
    listener.routeCount = routes.length;
    listener.backendCount = routes.reduce((total, route) => total + route.backendCount, 0);
    listener.fetchedAt = fetchedAt;
  }
  for (const bind of mockTrafficConfiguration.binds) {
    const listeners = mockTrafficConfiguration.listeners.filter(
      (listener) => listener.bindId === bind.id,
    );
    bind.listenerCount = listeners.length;
    bind.routeCount = listeners.reduce((total, listener) => total + listener.routeCount, 0);
    bind.backendCount = listeners.reduce((total, listener) => total + listener.backendCount, 0);
    bind.fetchedAt = fetchedAt;
  }
}

function trafficReceipt(operation: string, target: string, message: string) {
  nextTrafficRevision();
  const completedAt = new Date().toISOString();
  return HttpResponse.json({
    data: {
      operation,
      status: "succeeded",
      source: "agentgateway",
      target,
      requestId: `req_mock_${operation.replaceAll("-", "_")}`,
      completedAt,
      message,
    },
    meta: { ...meta("agentgateway"), fetchedAt: completedAt },
  });
}

function trafficListenerSetting(
  draft: TrafficListenerMutationRequest["listener"],
  bindId: string,
  current?: TrafficListenerSetting,
): TrafficListenerSetting {
  const bind = mockTrafficConfiguration.binds.find((item) => item.id === bindId);
  const configuration = structuredClone(draft.configuration);
  const name =
    typeof configuration.name === "string" && configuration.name
      ? configuration.name
      : "Unnamed listener";
  const protocol = ["HTTP", "HTTPS", "TLS", "TCP", "HBONE"].includes(String(configuration.protocol))
    ? (configuration.protocol as TrafficListenerSetting["protocol"])
    : "HTTP";
  return {
    id: current?.id ?? `mock-listener-${nextTrafficResource++}`,
    upstreamId: name,
    source: "agentgateway",
    fetchedAt: new Date().toISOString(),
    rawRef: current?.rawRef ?? { source: "agentgateway", id: `/binds/${bindId}/listeners/new` },
    bindId,
    port: bind?.port ?? current?.port ?? 0,
    name,
    hostname: typeof configuration.hostname === "string" ? configuration.hostname : "",
    protocol,
    routeCount: current?.routeCount ?? 0,
    backendCount: current?.backendCount ?? 0,
    configuration,
  };
}

function trafficRouteSetting(
  draft: TrafficRouteMutationRequest["route"],
  listenerId: string,
  current?: TrafficRouteSetting,
): TrafficRouteSetting {
  const listener = mockTrafficConfiguration.listeners.find((item) => item.id === listenerId);
  const configuration = structuredClone(draft.configuration);
  const backends = Array.isArray(configuration.backends) ? configuration.backends : [];
  const hostnames = Array.isArray(configuration.hostnames)
    ? configuration.hostnames.filter((item): item is string => typeof item === "string")
    : [];
  const name =
    (typeof configuration.name === "string" && configuration.name) ||
    (typeof configuration.ruleName === "string" && configuration.ruleName) ||
    "(unnamed)";
  return {
    id: current?.id ?? `mock-route-${nextTrafficResource++}`,
    upstreamId: name,
    source: "agentgateway",
    fetchedAt: new Date().toISOString(),
    rawRef: current?.rawRef ?? {
      source: "agentgateway",
      id: `/listeners/${listenerId}/routes/new`,
    },
    listenerId,
    listener: listener?.name ?? current?.listener ?? "Listener",
    port: listener?.port ?? current?.port ?? 0,
    kind: draft.kind,
    name,
    hostnames,
    backendCount: backends.length,
    configuration,
  };
}

function credentialState(
  draft: LlmProviderDraft | LlmModelDraft,
  current?: { credential: LlmCredentialState },
) {
  if (draft.credential.mode === "preserve")
    return current?.credential ?? { configured: false, kind: "ambient" as const };
  if (draft.credential.mode === "ambient") return { configured: false, kind: "ambient" as const };
  return { configured: true, kind: draft.credential.mode } as LlmCredentialState;
}

function providerSetting(
  draft: LlmProviderDraft,
  current?: LlmProviderSetting,
): LlmProviderSetting {
  const index = current
    ? mockLlmConfiguration.providers.findIndex((provider) => provider.id === current.id)
    : mockLlmConfiguration.providers.length;
  return {
    id: current?.id ?? `mock-provider-${nextLlmResource++}`,
    upstreamId: draft.name,
    source: "agentgateway",
    fetchedAt: new Date().toISOString(),
    rawRef: { source: "agentgateway", id: `/llm/providers/${Math.max(index, 0)}` },
    name: draft.name,
    providerType: draft.providerType,
    params: structuredClone(draft.params),
    formats: structuredClone(draft.formats),
    credential: credentialState(draft, current),
    modelCount: current?.modelCount ?? 0,
    editable: true,
  };
}

function modelSetting(draft: LlmModelDraft, current?: LlmModelSetting): LlmModelSetting {
  const index = current
    ? mockLlmConfiguration.models.findIndex((model) => model.id === current.id)
    : mockLlmConfiguration.models.length;
  return {
    id: current?.id ?? `mock-model-${nextLlmResource++}`,
    upstreamId: draft.name,
    source: "agentgateway",
    fetchedAt: new Date().toISOString(),
    rawRef: { source: "agentgateway", id: `/llm/models/${Math.max(index, 0)}` },
    name: draft.name,
    providerMode: draft.providerMode,
    providerType: draft.providerType,
    providerReference: draft.providerReference,
    params: structuredClone(draft.params),
    formats: structuredClone(draft.formats),
    visibility: draft.visibility,
    upstreamMode: draft.upstreamMode,
    modelExpression: draft.modelExpression,
    credential: credentialState(draft, current),
    editable: true,
  };
}

function llmReceipt(operation: string, target: string, message: string) {
  nextLlmRevision();
  const completedAt = new Date().toISOString();
  return HttpResponse.json({
    data: {
      operation,
      status: "succeeded",
      source: "agentgateway",
      target,
      requestId: `req_mock_${operation.replaceAll("-", "_")}`,
      completedAt,
      message,
    },
    meta: { ...meta("agentgateway"), fetchedAt: completedAt },
  });
}
const mockScanJobs = new Map(trustScans.map((job) => [job.id, structuredClone(job)]));
const mockScanPolls = new Map<string, number>();
const mockScanAttempts = new Map<string, number>();
let nextScan = 300;

function currentOverview(): OverviewData {
  return structuredClone(overviewData);
}

function trustJobEnvelope(job: TrustScanJob) {
  return { data: job, meta: meta("agentguard", job.status === "failed") };
}

async function startMockScan(request: Request, agentId: string, resourceType: "skill" | "mcp") {
  const body = (await request.json()) as SkillDetectionRequest | MCPDetectionRequest;
  const resourceIds = body.resourceIds ?? [];
  const attemptKey = `${resourceType}:${resourceIds.join(",")}`;
  const attempt = (mockScanAttempts.get(attemptKey) ?? 0) + 1;
  mockScanAttempts.set(attemptKey, attempt);
  const job: TrustScanJob = {
    id: `scan-mock-${nextScan++}`,
    source: "agentguard",
    agentId,
    agentUpstreamId:
      trustAgents.find((agent) => agent.id === agentId)?.upstreamId ?? "unknown-agent",
    resourceType,
    resourceIds,
    status: "queued",
    createdAt: new Date().toISOString(),
    startedAt: null,
    completedAt: null,
    updatedAt: new Date().toISOString(),
    results: [],
    warnings: [],
  };
  if (scenarioFrom(request) === "partial" && attempt === 1) {
    job.warnings = ["Mock failure is enabled once for recovery testing."];
  }
  mockScanJobs.set(job.id, job);
  mockScanPolls.set(job.id, 0);
  return HttpResponse.json(trustJobEnvelope(job), { status: 202 });
}

const traceCapturedAt = "2026-07-30T08:10:00Z";
let liveTraceRevision = 0;

function traceMeta() {
  return { fetchedAt: traceCapturedAt, stale: false, partial: false };
}

function traceFailure(status: number, code: string, message: string): Response {
  return HttpResponse.json(
    {
      error: {
        code,
        message,
        requestId: `req_mock_trace_${status}`,
        retryable: status >= 500,
      },
    } satisfies ApiFailure,
    { status },
  );
}

function advanceRunningTrace(): TraceSummary | undefined {
  const index = mockTraceSummaries.findIndex((trace) => trace.status === "running");
  if (index < 0) return undefined;

  const current = mockTraceSummaries[index];
  const detail = mockTraceDetails[current.traceId];
  const template = detail?.spans.at(-1);
  if (!detail || !template) return undefined;

  liveTraceRevision += 1;
  const timestamp = new Date().toISOString();
  const span: TraceSpan = {
    ...template,
    spanId: `23${liveTraceRevision.toString(16).padStart(14, "0")}`,
    parentSpanId: current.rootSpanId,
    name: `Live tool update ${liveTraceRevision}`,
    openInferenceKind: "TOOL",
    startedAt: timestamp,
    endedAt: null,
    durationMs: null,
    statusCode: "unset",
    provider: undefined,
    model: undefined,
    toolName: "ops.live-update",
    toolKind: "local",
    inputTokens: null,
    outputTokens: null,
    totalTokens: null,
    contentState: "not_collected",
    receivedAt: timestamp,
    updatedAt: timestamp,
  };
  const summary: TraceSummary = {
    ...current,
    toolCalls: current.toolCalls + 1,
    localToolCalls: current.localToolCalls + 1,
    spanCount: current.spanCount + 1,
    lastSpanAt: timestamp,
    updatedAt: timestamp,
  };

  mockTraceSummaries[index] = summary;
  mockTraceDetails[current.traceId] = {
    ...detail,
    summary,
    spans: [...detail.spans, span],
    coverage: {
      ...detail.coverage,
      spanKinds: [...new Set([...detail.coverage.spanKinds, "TOOL"])],
    },
    totalSpans: detail.totalSpans + 1,
  };
  mockTraceSpanDetails[`${span.traceId}:${span.spanId}`] = {
    span,
    attributes: {
      "agentshark.agent.id": span.agentId,
      "agentshark.session.id": span.sessionId,
      "agentshark.task.id": span.taskId,
      "tool.name": span.toolName,
      "tool.kind": span.toolKind,
    },
    resource: { "service.name": "agentsharkx-mock-runtime" },
    events: [],
    payloads: [],
  };
  return summary;
}

async function traceListResponse(request: Request): Promise<Response> {
  const scenario = scenarioFrom(request);
  if (scenario === "loading") await delay(30_000);
  if (scenario === "error") {
    return traceFailure(503, "DATABASE_UNAVAILABLE", "Trace storage is temporarily unavailable.");
  }
  const url = new URL(request.url);
  let items = scenario === "empty" ? [] : [...mockTraceSummaries];
  if (scenario === "partial") {
    items = items.filter((trace) => trace.status === "running" || trace.completeness === "partial");
  }
  const status = url.searchParams.get("status");
  const completeness = url.searchParams.get("completeness");
  const agentId = url.searchParams.get("agent_id");
  const sessionId = url.searchParams.get("session_id");
  const taskId = url.searchParams.get("task_id");
  const hasError = url.searchParams.get("has_error");
  const hasA2A = url.searchParams.get("has_a2a");
  const startedAfter = url.searchParams.get("started_after");
  const startedBefore = url.searchParams.get("started_before");
  const query = (url.searchParams.get("query") ?? "").trim().toLowerCase();
  items = items.filter((trace) => {
    if (status && trace.status !== status) return false;
    if (completeness && trace.completeness !== completeness) return false;
    if (agentId && trace.rootAgentId !== agentId) return false;
    if (sessionId && trace.sessionId !== sessionId) return false;
    if (taskId && trace.taskId !== taskId) return false;
    if (hasError === "true" && trace.errorCount === 0) return false;
    if (hasError === "false" && trace.errorCount > 0) return false;
    if (hasA2A === "true" && trace.a2aCalls === 0) return false;
    if (hasA2A === "false" && trace.a2aCalls > 0) return false;
    if (startedAfter && trace.startedAt < startedAfter) return false;
    if (startedBefore && trace.startedAt >= startedBefore) return false;
    if (
      query &&
      ![trace.traceId, trace.taskId, trace.sessionId]
        .filter(Boolean)
        .join(" ")
        .toLowerCase()
        .includes(query)
    )
      return false;
    return true;
  });
  items.sort((left, right) =>
    right.startedAt === left.startedAt
      ? right.traceId.localeCompare(left.traceId)
      : right.startedAt.localeCompare(left.startedAt),
  );
  const cursor = url.searchParams.get("cursor");
  const match = cursor?.match(/^mock:(\d+)$/);
  if (cursor && !match) return traceFailure(400, "INVALID_CURSOR", "The Trace cursor is invalid.");
  const offset = Number(match?.[1] ?? 0);
  const limit = Math.max(1, Math.min(100, Number(url.searchParams.get("limit") ?? 25) || 25));
  const page = items.slice(offset, offset + limit);
  return HttpResponse.json({
    data: {
      items: page,
      nextCursor: offset + limit < items.length ? `mock:${offset + limit}` : null,
      total: items.length,
    },
    meta: traceMeta(),
  });
}

export const handlers = [
  http.get(
    "/api/v1/auth/session",
    () => new HttpResponse(null, { status: 204, headers: { "X-CSRF-Token": "mock-csrf" } }),
  ),
  http.get("/api/v1/overview", ({ request }) => respond(request, currentOverview(), emptyOverview)),
  http.get("/api/v1/system/health", ({ request }) => {
    const health =
      scenarioFrom(request) === "partial"
        ? overviewData.health.map((source) =>
            source.source === "agentguard"
              ? {
                  ...source,
                  status: "down" as const,
                  latencyMs: null,
                  message: "AgentGuard mock probe timed out",
                }
              : source,
          )
        : overviewData.health;
    return listResponse(request, health, "agentgateway");
  }),
  http.get("/api/v1/system/capabilities", ({ request }) =>
    listResponse(request, capabilityData, "agentgateway"),
  ),
  http.get("/api/v1/system/diagnostics", ({ request }) => {
    const partial = scenarioFrom(request) === "partial";
    const data: DiagnosticsData = {
      status: partial ? "degraded" : "healthy",
      issues: partial
        ? [
            {
              source: "agentguard",
              status: "down",
              summary:
                "AgentGuard management probes are unavailable. Trust, runtime protection, and approvals may be incomplete.",
              checks: [
                "Confirm the configured base URL reaches the upstream management port from the AgentsharkX container.",
                "Inspect the upstream container health and logs, then retry the probe.",
                "Verify AGENTGUARD_BASE_URL and that AGENTGUARD_ADMIN_TOKEN matches the AgentGuard API key.",
                "Confirm GET /v1/backend/health succeeds from the AgentsharkX network.",
              ],
              documentationPath:
                "https://github.com/Thespectier/AgentsharkX/blob/main/docs/troubleshooting.md#upstream-connectivity",
            },
          ]
        : [],
    };
    return respond(request, data, { status: "healthy", issues: [] }, "agentguard");
  }),
  http.get("/api/v1/connect/summary", ({ request }) =>
    respond(request, connectData.summary, emptyConnectSummary, "agentgateway"),
  ),
  http.get("/api/v1/connect/analytics", ({ request }) =>
    respond(request, connectData.summary.analytics, emptyConnectSummary.analytics, "agentgateway"),
  ),
  http.get("/api/v1/connect/setup", ({ request }) =>
    respond(
      request,
      {
        source: "agentgateway" as const,
        managementConfigured: true,
        configurationReadable: true,
        status: "healthy" as const,
        version: "1.3.1",
        latencyMs: 18,
        checkedAt: capturedAt,
        links: connectData.summary.links,
      },
      {
        source: "agentgateway" as const,
        managementConfigured: true,
        configurationReadable: true,
        status: "healthy" as const,
        version: "1.3.1",
        latencyMs: 18,
        checkedAt: capturedAt,
        links: connectData.summary.links,
      },
      "agentgateway",
    ),
  ),
  http.get("/api/v1/connect/llm/configuration", ({ request }) =>
    respond(
      request,
      structuredClone(mockLlmConfiguration),
      { ...structuredClone(mockLlmConfiguration), providers: [], models: [], virtualModels: [] },
      "agentgateway",
    ),
  ),
  http.post("/api/v1/connect/llm/providers", async ({ request }) => {
    const body = (await request.json()) as LlmProviderMutationRequest;
    const conflict = acceptLlmRevision(body.revisionToken);
    if (conflict) return conflict;
    if (mockLlmConfiguration.providers.some((provider) => provider.name === body.provider.name)) {
      return llmFailure(409, "RESOURCE_CONFLICT", "A provider with this name already exists.");
    }
    mockLlmConfiguration.providers.push(providerSetting(body.provider));
    return llmReceipt(
      "create-llm-provider",
      body.provider.name,
      "Provider created in agentgateway.",
    );
  }),
  http.patch("/api/v1/connect/llm/providers/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as LlmProviderMutationRequest;
    const conflict = acceptLlmRevision(body.revisionToken);
    if (conflict) return conflict;
    const index = mockLlmConfiguration.providers.findIndex(
      (provider) => provider.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Provider was not found.");
    const current = mockLlmConfiguration.providers[index];
    mockLlmConfiguration.providers[index] = providerSetting(body.provider, current);
    return llmReceipt(
      "update-llm-provider",
      body.provider.name,
      "Provider updated in agentgateway.",
    );
  }),
  http.delete("/api/v1/connect/llm/providers/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as LlmProviderDeleteRequest;
    const conflict = acceptLlmRevision(body.revisionToken);
    if (conflict) return conflict;
    if (!body.confirmed) return llmFailure(400, "INVALID_REQUEST", "Deletion must be confirmed.");
    const index = mockLlmConfiguration.providers.findIndex(
      (provider) => provider.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Provider was not found.");
    const provider = mockLlmConfiguration.providers[index];
    const referencedModels = mockLlmConfiguration.models.filter(
      (model) => model.providerMode === "reference" && model.providerReference === provider.name,
    );
    if (referencedModels.length > 0 && !body.deleteReferencedModels) {
      return llmFailure(409, "RESOURCE_REFERENCED", "Provider is referenced by a model.");
    }
    const virtualReferences = new Set(
      mockLlmConfiguration.virtualModels.flatMap((model) => model.targets ?? []),
    );
    if (referencedModels.some((model) => virtualReferences.has(model.name))) {
      return llmFailure(
        409,
        "RESOURCE_REFERENCED",
        "A referenced model is used by a virtual model.",
      );
    }
    const referencedIDs = new Set(referencedModels.map((model) => model.id));
    mockLlmConfiguration.models = mockLlmConfiguration.models.filter(
      (model) => !referencedIDs.has(model.id),
    );
    mockLlmConfiguration.providers.splice(index, 1);
    return llmReceipt("delete-llm-provider", provider.name, "Provider deleted from agentgateway.");
  }),
  http.post("/api/v1/connect/llm/models", async ({ request }) => {
    const body = (await request.json()) as LlmModelMutationRequest;
    const conflict = acceptLlmRevision(body.revisionToken);
    if (conflict) return conflict;
    if (mockLlmConfiguration.models.some((model) => model.name === body.model.name)) {
      return llmFailure(409, "RESOURCE_CONFLICT", "A model with this name already exists.");
    }
    mockLlmConfiguration.models.push(modelSetting(body.model));
    return llmReceipt("create-llm-model", body.model.name, "Model created in agentgateway.");
  }),
  http.patch("/api/v1/connect/llm/models/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as LlmModelMutationRequest;
    const conflict = acceptLlmRevision(body.revisionToken);
    if (conflict) return conflict;
    const index = mockLlmConfiguration.models.findIndex(
      (model) => model.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Model was not found.");
    const current = mockLlmConfiguration.models[index];
    mockLlmConfiguration.models[index] = modelSetting(body.model, current);
    return llmReceipt("update-llm-model", body.model.name, "Model updated in agentgateway.");
  }),
  http.delete("/api/v1/connect/llm/models/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as LlmDeleteRequest;
    const conflict = acceptLlmRevision(body.revisionToken);
    if (conflict) return conflict;
    if (!body.confirmed) return llmFailure(400, "INVALID_REQUEST", "Deletion must be confirmed.");
    const index = mockLlmConfiguration.models.findIndex(
      (model) => model.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Model was not found.");
    const model = mockLlmConfiguration.models[index];
    const referenced = mockLlmConfiguration.virtualModels.some((virtualModel) =>
      virtualModel.targets?.includes(model.name),
    );
    if (referenced)
      return llmFailure(409, "RESOURCE_REFERENCED", "Model is referenced by a virtual model.");
    mockLlmConfiguration.models.splice(index, 1);
    return llmReceipt("delete-llm-model", model.name, "Model deleted from agentgateway.");
  }),
  http.get("/api/v1/connect/llm/providers", ({ request }) =>
    pageResponse(request, connectData.providers, "agentgateway"),
  ),
  http.get("/api/v1/connect/llm/providers/:resourceId", ({ request, params }) => {
    const item = connectData.providers.find((provider) => provider.id === params.resourceId);
    return item ? respond(request, item, item, "agentgateway") : failure("agentgateway");
  }),
  http.get("/api/v1/connect/llm/models", ({ request }) =>
    pageResponse(request, connectData.models, "agentgateway"),
  ),
  http.get("/api/v1/connect/llm/models/:resourceId", ({ request, params }) => {
    const item = connectData.models.find((model) => model.id === params.resourceId);
    return item ? respond(request, item, item, "agentgateway") : failure("agentgateway");
  }),
  http.get("/api/v1/connect/mcp/configuration", ({ request }) =>
    respond(
      request,
      structuredClone(mockMcpConfiguration),
      { ...structuredClone(mockMcpConfiguration), servers: [], inlineServers: [] },
      "agentgateway",
    ),
  ),
  http.patch("/api/v1/connect/mcp/configuration/settings", async ({ request }) => {
    const body = (await request.json()) as McpSettingsMutationRequest;
    const conflict = acceptMcpRevision(body.revisionToken);
    if (conflict) return conflict;
    mockMcpConfiguration.settings = {
      ...structuredClone(body.settings),
      hasPolicies: mockMcpConfiguration.settings.hasPolicies,
    };
    return mcpReceipt("update-mcp-settings", "MCP settings", "MCP settings updated");
  }),
  http.post("/api/v1/connect/mcp/servers", async ({ request }) => {
    const body = (await request.json()) as McpServerMutationRequest;
    const conflict = acceptMcpRevision(body.revisionToken);
    if (conflict) return conflict;
    if (mockMcpConfiguration.servers.some((server) => server.name === body.server.name))
      return llmFailure(409, "RESOURCE_CONFLICT", "An MCP server with this name already exists.");
    mockMcpConfiguration.servers.push(mcpServerSetting(body.server));
    return mcpReceipt("create-mcp-server", body.server.name, "MCP server created");
  }),
  http.patch("/api/v1/connect/mcp/servers/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as McpServerMutationRequest;
    const conflict = acceptMcpRevision(body.revisionToken);
    if (conflict) return conflict;
    const index = mockMcpConfiguration.servers.findIndex(
      (server) => server.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "MCP server was not found.");
    const current = mockMcpConfiguration.servers[index];
    if (!current.editable)
      return llmFailure(400, "INVALID_REQUEST", "This MCP server uses advanced configuration.");
    if (
      mockMcpConfiguration.servers.some(
        (server, candidate) => candidate !== index && server.name === body.server.name,
      )
    )
      return llmFailure(409, "RESOURCE_CONFLICT", "An MCP server with this name already exists.");
    mockMcpConfiguration.servers[index] = mcpServerSetting(body.server, current);
    return mcpReceipt("update-mcp-server", body.server.name, "MCP server updated");
  }),
  http.delete("/api/v1/connect/mcp/servers/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as McpDeleteRequest;
    const conflict = acceptMcpRevision(body.revisionToken);
    if (conflict) return conflict;
    if (!body.confirmed) return llmFailure(400, "INVALID_REQUEST", "Deletion must be confirmed.");
    const index = mockMcpConfiguration.servers.findIndex(
      (server) => server.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "MCP server was not found.");
    const target = mockMcpConfiguration.servers[index];
    if (!target.editable)
      return llmFailure(400, "INVALID_REQUEST", "This MCP server uses advanced configuration.");
    mockMcpConfiguration.servers.splice(index, 1);
    return mcpReceipt("delete-mcp-server", target.name, "MCP server deleted");
  }),
  http.get("/api/v1/connect/mcp/servers", ({ request }) =>
    pageResponse(request, connectData.mcpServers, "agentgateway"),
  ),
  http.get("/api/v1/connect/mcp/servers/:resourceId", ({ request, params }) => {
    const item = connectData.mcpServers.find((server) => server.id === params.resourceId);
    return item ? respond(request, item, item, "agentgateway") : failure("agentgateway");
  }),
  http.get("/api/v1/connect/traffic/configuration", ({ request }) =>
    respond(
      request,
      structuredClone(mockTrafficConfiguration),
      {
        ...structuredClone(mockTrafficConfiguration),
        binds: [],
        listeners: [],
        routes: [],
      },
      "agentgateway",
    ),
  ),
  http.post("/api/v1/connect/traffic/binds", async ({ request }) => {
    const body = (await request.json()) as TrafficBindMutationRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    if (mockTrafficConfiguration.binds.some((bind) => bind.port === body.bind.port))
      return llmFailure(409, "RESOURCE_CONFLICT", "A bind with this port already exists.");
    const id = `mock-bind-${nextTrafficResource++}`;
    mockTrafficConfiguration.binds.push({
      id,
      upstreamId: String(body.bind.port),
      source: "agentgateway",
      fetchedAt: new Date().toISOString(),
      rawRef: { source: "agentgateway", id: `/binds/${mockTrafficConfiguration.binds.length}` },
      port: body.bind.port,
      tunnelProtocol: "direct",
      listenerCount: 0,
      routeCount: 0,
      backendCount: 0,
    });
    return trafficReceipt("create-traffic-bind", `Port ${body.bind.port}`, "Traffic bind created");
  }),
  http.patch("/api/v1/connect/traffic/binds/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as TrafficBindMutationRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    const bind = mockTrafficConfiguration.binds.find(
      (item) => item.id === String(params.resourceId),
    );
    if (!bind) return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic bind was not found.");
    if (
      mockTrafficConfiguration.binds.some(
        (item) => item.id !== bind.id && item.port === body.bind.port,
      )
    )
      return llmFailure(409, "RESOURCE_CONFLICT", "A bind with this port already exists.");
    bind.port = body.bind.port;
    bind.upstreamId = String(body.bind.port);
    for (const listener of mockTrafficConfiguration.listeners.filter(
      (item) => item.bindId === bind.id,
    ))
      listener.port = bind.port;
    return trafficReceipt("update-traffic-bind", `Port ${bind.port}`, "Traffic bind updated");
  }),
  http.delete("/api/v1/connect/traffic/binds/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as TrafficDeleteRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    if (!body.confirmed) return llmFailure(400, "INVALID_REQUEST", "Deletion must be confirmed.");
    const index = mockTrafficConfiguration.binds.findIndex(
      (item) => item.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic bind was not found.");
    if (mockTrafficConfiguration.binds[index].listenerCount && !body.deleteChildren)
      return llmFailure(409, "RESOURCE_REFERENCED", "Traffic bind still has listeners.");
    const [bind] = mockTrafficConfiguration.binds.splice(index, 1);
    const listenerIds = new Set(
      mockTrafficConfiguration.listeners
        .filter((item) => item.bindId === bind.id)
        .map((item) => item.id),
    );
    mockTrafficConfiguration.listeners = mockTrafficConfiguration.listeners.filter(
      (item) => item.bindId !== bind.id,
    );
    mockTrafficConfiguration.routes = mockTrafficConfiguration.routes.filter(
      (item) => !listenerIds.has(item.listenerId),
    );
    return trafficReceipt("delete-traffic-bind", `Port ${bind.port}`, "Traffic bind deleted");
  }),
  http.post("/api/v1/connect/traffic/listeners", async ({ request }) => {
    const body = (await request.json()) as TrafficListenerMutationRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    const bindId = body.bindId ?? "";
    if (!mockTrafficConfiguration.binds.some((bind) => bind.id === bindId))
      return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic bind was not found.");
    const listener = trafficListenerSetting(body.listener, bindId);
    mockTrafficConfiguration.listeners.push(listener);
    return trafficReceipt("create-traffic-listener", listener.name, "Traffic listener created");
  }),
  http.patch("/api/v1/connect/traffic/listeners/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as TrafficListenerMutationRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    const index = mockTrafficConfiguration.listeners.findIndex(
      (item) => item.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic listener was not found.");
    const current = mockTrafficConfiguration.listeners[index];
    const next = trafficListenerSetting(body.listener, current.bindId, current);
    const changedKind =
      (current.protocol === "TCP" || current.protocol === "TLS") !==
      (next.protocol === "TCP" || next.protocol === "TLS");
    if (changedKind && current.routeCount && !body.listener.deleteIncompatibleRoutes)
      return llmFailure(409, "RESOURCE_REFERENCED", "Listener still has incompatible routes.");
    if (changedKind)
      mockTrafficConfiguration.routes = mockTrafficConfiguration.routes.filter(
        (route) => route.listenerId !== current.id,
      );
    mockTrafficConfiguration.listeners[index] = next;
    for (const route of mockTrafficConfiguration.routes.filter(
      (item) => item.listenerId === next.id,
    ))
      route.listener = next.name;
    return trafficReceipt("update-traffic-listener", next.name, "Traffic listener updated");
  }),
  http.delete("/api/v1/connect/traffic/listeners/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as TrafficDeleteRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    if (!body.confirmed) return llmFailure(400, "INVALID_REQUEST", "Deletion must be confirmed.");
    const index = mockTrafficConfiguration.listeners.findIndex(
      (item) => item.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic listener was not found.");
    if (mockTrafficConfiguration.listeners[index].routeCount && !body.deleteChildren)
      return llmFailure(409, "RESOURCE_REFERENCED", "Traffic listener still has routes.");
    const [listener] = mockTrafficConfiguration.listeners.splice(index, 1);
    mockTrafficConfiguration.routes = mockTrafficConfiguration.routes.filter(
      (item) => item.listenerId !== listener.id,
    );
    return trafficReceipt("delete-traffic-listener", listener.name, "Traffic listener deleted");
  }),
  http.post("/api/v1/connect/traffic/routes", async ({ request }) => {
    const body = (await request.json()) as TrafficRouteMutationRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    const listenerId = body.listenerId ?? "";
    if (!mockTrafficConfiguration.listeners.some((listener) => listener.id === listenerId))
      return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic listener was not found.");
    const route = trafficRouteSetting(body.route, listenerId);
    mockTrafficConfiguration.routes.push(route);
    return trafficReceipt("create-traffic-route", route.name, "Traffic route created");
  }),
  http.patch("/api/v1/connect/traffic/routes/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as TrafficRouteMutationRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    const index = mockTrafficConfiguration.routes.findIndex(
      (item) => item.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic route was not found.");
    const current = mockTrafficConfiguration.routes[index];
    const route = trafficRouteSetting(body.route, current.listenerId, current);
    mockTrafficConfiguration.routes[index] = route;
    return trafficReceipt("update-traffic-route", route.name, "Traffic route updated");
  }),
  http.delete("/api/v1/connect/traffic/routes/:resourceId", async ({ request, params }) => {
    const body = (await request.json()) as TrafficDeleteRequest;
    const conflict = acceptTrafficRevision(body.revisionToken);
    if (conflict) return conflict;
    if (!body.confirmed) return llmFailure(400, "INVALID_REQUEST", "Deletion must be confirmed.");
    const index = mockTrafficConfiguration.routes.findIndex(
      (item) => item.id === String(params.resourceId),
    );
    if (index < 0) return llmFailure(404, "RESOURCE_NOT_FOUND", "Traffic route was not found.");
    const [route] = mockTrafficConfiguration.routes.splice(index, 1);
    return trafficReceipt("delete-traffic-route", route.name, "Traffic route deleted");
  }),
  http.get("/api/v1/connect/traffic/routes", ({ request }) =>
    pageResponse(request, connectData.routes, "agentgateway"),
  ),
  http.get("/api/v1/connect/traffic/routes/:resourceId", ({ request, params }) => {
    const item = connectData.routes.find((route) => route.id === params.resourceId);
    return item ? respond(request, item, item, "agentgateway") : failure("agentgateway");
  }),
  http.get("/api/v1/trust/agents", ({ request }) =>
    pageResponse(request, trustAgents, "agentguard"),
  ),
  http.get("/api/v1/trust/agents/:agentId", ({ request, params }) => {
    const agent = trustAgents.find((item) => item.id === params.agentId);
    if (!agent) {
      return HttpResponse.json(
        {
          error: {
            code: "NOT_FOUND",
            message: "Agent was not found in explicit AgentGuard data",
            source: "agentguard",
            requestId: "req_mock_agent_404",
            retryable: false,
          },
        } satisfies ApiFailure,
        { status: 404 },
      );
    }
    const resources = mockTrustResources.filter((item) => item.ownerAgentId === agent.id);
    return respond(
      request,
      {
        agent,
        sessions: Array.from({ length: agent.sessions }, (_, index) => ({
          id: `session-${agent.id}-${index}`,
          upstreamId: `session-${index + 1}`,
          source: "agentguard" as const,
          fetchedAt: capturedAt,
          rawRef: { source: "agentguard" as const, id: `/v1/backend/sessions/sessions/${index}` },
          agentId: agent.id,
          agentUpstreamId: agent.upstreamId,
          userId: agent.principal ?? undefined,
          lastSeen: agent.lastActive,
          status: "unknown" as const,
        })),
        resources,
      },
      { agent, sessions: [], resources: [] },
      "agentguard",
    );
  }),
  http.get("/api/v1/trust/resources", ({ request }) => {
    const url = new URL(request.url);
    const resourceType = url.searchParams.get("type");
    const agentId = url.searchParams.get("agentId");
    const resources = mockTrustResources.filter(
      (item) =>
        (!resourceType || item.type === resourceType) &&
        (!agentId || item.ownerAgentId === agentId),
    );
    return pageResponse(request, resources, "agentguard");
  }),
  http.patch("/api/v1/trust/agents/:agentId/tools/:tool/labels", async ({ request, params }) => {
    const index = mockTrustResources.findIndex(
      (item) =>
        item.id === params.tool && item.ownerAgentId === params.agentId && item.type === "tool",
    );
    if (index < 0) return failure("agentguard");
    const body = (await request.json()) as LabelUpdate;
    await delay(250);
    const current = mockTrustResources[index];
    const updated: TrustResource = {
      ...current,
      fetchedAt: new Date().toISOString(),
      labels: {
        boundary: body.boundary ? "server-confirmed" : (current.labels?.boundary ?? "unknown"),
        sensitivity: body.sensitivity ?? current.labels?.sensitivity ?? "unknown",
        integrity: body.integrity ?? current.labels?.integrity ?? "unknown",
        tags: body.tags ?? current.labels?.tags ?? [],
      },
    };
    mockTrustResources[index] = updated;
    return HttpResponse.json({ data: updated, meta: meta("agentguard") });
  }),
  http.post("/api/v1/trust/agents/:agentId/skills/detect", ({ request, params }) =>
    startMockScan(request, String(params.agentId), "skill"),
  ),
  http.post("/api/v1/trust/agents/:agentId/mcps/detect", ({ request, params }) =>
    startMockScan(request, String(params.agentId), "mcp"),
  ),
  http.get("/api/v1/trust/scans", ({ request }) =>
    pageResponse(request, [...mockScanJobs.values()].reverse(), "agentguard"),
  ),
  http.get("/api/v1/trust/scans/:scanId", ({ params }) => {
    const id = String(params.scanId);
    const current = mockScanJobs.get(id);
    if (!current) return failure("agentguard");
    if (current.status === "queued" || current.status === "running") {
      const polls = (mockScanPolls.get(id) ?? 0) + 1;
      mockScanPolls.set(id, polls);
      const now = new Date().toISOString();
      current.status = polls === 1 ? "running" : "succeeded";
      current.startedAt ??= now;
      current.updatedAt = now;
      if (polls > 1) {
        const shouldFail = current.warnings.some((warning) => warning.includes("recovery testing"));
        current.status = shouldFail ? "failed" : "succeeded";
        current.completedAt = now;
        if (shouldFail) {
          current.error = {
            code: "UPSTREAM_UNAVAILABLE",
            message: "Mock AgentGuard detector became unavailable",
            retryable: true,
          };
        } else {
          current.results = current.resourceIds.flatMap((resourceId) => {
            const detection = mockTrustResources.find(
              (resource) => resource.id === resourceId,
            )?.detection;
            return detection ? [detection] : [];
          });
        }
      }
      mockScanJobs.set(id, current);
    }
    return HttpResponse.json(trustJobEnvelope(current));
  }),
  http.get("/api/v1/protect/policies", ({ request }) =>
    respond(request, mockProtectSnapshot, emptyProtect),
  ),
  http.get("/api/v1/protect/gateway-policies/configuration", ({ request }) =>
    respond(
      request,
      structuredClone(mockGatewayPolicyConfiguration),
      { ...structuredClone(mockGatewayPolicyConfiguration), settings: [] },
      "agentgateway",
    ),
  ),
  http.patch("/api/v1/protect/gateway-policies/:resourceId", async ({ request, params }) => {
    const input = (await request.json()) as GatewayPolicyMutationRequest;
    const conflict = acceptGatewayPolicyRevision(input.revisionToken);
    if (conflict) return conflict;
    if (input.value === null || input.value === undefined) {
      return llmFailure(400, "INVALID_REQUEST", "A non-null JSON policy value is required.");
    }
    const setting = mockGatewayPolicyConfiguration.settings.find(
      (item) => item.id === String(params.resourceId),
    );
    if (!setting) return llmFailure(404, "NOT_FOUND", "Gateway policy was not found.");
    if (!setting.editable) {
      return llmFailure(400, "INVALID_REQUEST", "This source-owned policy is read-only.");
    }
    await delay(120);
    setting.value = structuredClone(input.value);
    setting.enabled = true;
    setting.fetchedAt = new Date().toISOString();
    return gatewayPolicyReceipt("upsert-gateway-policy", setting.rawRef.id, "Gateway policy saved");
  }),
  http.delete("/api/v1/protect/gateway-policies/:resourceId", async ({ request, params }) => {
    const input = (await request.json()) as GatewayPolicyDeleteRequest;
    if (!input.confirmed) {
      return llmFailure(400, "INVALID_REQUEST", "Explicit confirmation is required.");
    }
    const conflict = acceptGatewayPolicyRevision(input.revisionToken);
    if (conflict) return conflict;
    const setting = mockGatewayPolicyConfiguration.settings.find(
      (item) => item.id === String(params.resourceId),
    );
    if (!setting) return llmFailure(404, "NOT_FOUND", "Gateway policy was not found.");
    if (!setting.editable) {
      return llmFailure(400, "INVALID_REQUEST", "This source-owned policy is read-only.");
    }
    await delay(120);
    setting.value = null;
    setting.enabled = false;
    setting.fetchedAt = new Date().toISOString();
    return gatewayPolicyReceipt(
      "delete-gateway-policy",
      setting.rawRef.id,
      "Gateway policy deleted",
    );
  }),
  http.post("/api/v1/protect/runtime-rules/check", async ({ request }) => {
    const scenario = scenarioFrom(request);
    if (scenario === "loading") await delay(30_000);
    if (scenario === "error") return failure("agentguard");
    const input = (await request.json()) as RuntimeRuleCheckRequest;
    const publishable = input.source.includes("RULE") && !input.source.includes("INVALID");
    const token = publishable ? `check-${Date.now()}-${ruleChecks.size}` : undefined;
    if (token) ruleChecks.set(token, input.source);
    return HttpResponse.json({
      data: {
        source: "agentguard",
        ok: publishable,
        publishable,
        ruleCount: publishable ? 1 : 0,
        errors: publishable ? [] : [{ message: "Expected exactly one valid RULE block." }],
        warnings: [],
        hints: publishable ? [{ message: "Rule is ready for explicit publication." }] : [],
        checkToken: token,
        expiresAt: publishable ? new Date(Date.now() + 300_000).toISOString() : null,
        requestId: "req_mock_rule_check",
      },
      meta: meta("agentguard"),
    });
  }),
  http.post("/api/v1/protect/agents/:agentId/runtime-rules", async ({ request, params }) => {
    const input = (await request.json()) as RuntimeRulePublishRequest;
    await delay(120);
    const checkedSource = ruleChecks.get(input.checkToken);
    if (!input.confirmed || !input.note.trim()) {
      return protectFailure(
        400,
        "INVALID_REQUEST",
        "Confirmation and an operator note are required.",
      );
    }
    if (checkedSource !== input.source) {
      return protectFailure(
        409,
        "RULE_CHECK_REQUIRED",
        "Run a successful syntax check immediately before publishing.",
      );
    }
    ruleChecks.delete(input.checkToken);
    const agentId = String(params.agentId);
    const agent = mockProtectSnapshot.plugins.find((item) => item.agentId === agentId);
    if (!agent)
      return protectFailure(404, "NOT_FOUND", "The explicit AgentGuard agent was not found.");
    const id = `rule-mock-${Date.now()}`;
    const created: RuntimeRule = {
      id,
      upstreamId: id,
      source: "agentguard",
      fetchedAt: new Date().toISOString(),
      rawRef: { source: "agentguard", id: `/v1/backend/rules/${id}` },
      name: "New checked runtime rule",
      agentId,
      agentUpstreamId: agent.agentUpstreamId,
      scope: "Agent runtime",
      phase: "unknown",
      action: "ALLOW",
      status: "published",
      userManaged: true,
    };
    mockProtectSnapshot.runtimeRules = [created, ...mockProtectSnapshot.runtimeRules];
    return protectReceipt("publish-runtime-rule", id, "Runtime rule published");
  }),
  http.delete(
    "/api/v1/protect/agents/:agentId/runtime-rules/:ruleId",
    async ({ request, params }) => {
      const input = (await request.json()) as ConfirmedActionRequest;
      await delay(120);
      if (!input.confirmed || !input.note.trim()) {
        return protectFailure(
          400,
          "INVALID_REQUEST",
          "Confirmation and an operator note are required.",
        );
      }
      const ruleId = String(params.ruleId);
      const index = mockProtectSnapshot.runtimeRules.findIndex(
        (item) => item.id === ruleId && item.agentId === String(params.agentId) && item.userManaged,
      );
      if (index < 0)
        return protectFailure(404, "NOT_FOUND", "The runtime rule is no longer available.");
      mockProtectSnapshot.runtimeRules.splice(index, 1);
      return protectReceipt("delete-runtime-rule", ruleId, "Runtime rule deleted");
    },
  ),
  http.get("/api/v1/demo/status", async ({ request }) => {
    const scenario = scenarioFrom(request);
    if (scenario === "loading") await delay(30_000);
    if (scenario === "error") {
      return demoFailure(503, "DEMO_UNAVAILABLE", "Demo Lab status is unavailable.", true);
    }
    const active = mockDemoActiveRun();
    if (scenario === "empty") {
      return HttpResponse.json({
        data: {
          ...structuredClone(demoStatus),
          enabled: false,
          ready: false,
          activeRunId: null,
          components: demoStatus.components.map((component) => ({
            ...component,
            status: "unknown" as const,
            message: "Demo Lab is disabled",
          })),
        },
        meta: meta(),
      });
    }
    if (scenario === "partial") {
      return HttpResponse.json({
        data: {
          ...structuredClone(demoStatus),
          ready: false,
          activeRunId: active?.runId ?? null,
          components: demoStatus.components.map((component) =>
            component.id === "demo-runner"
              ? {
                  ...component,
                  status: "down" as const,
                  message: "Demo Runner did not answer its readiness probe.",
                }
              : component,
          ),
        },
        meta: meta(),
      });
    }
    return HttpResponse.json({
      data: { ...structuredClone(demoStatus), activeRunId: active?.runId ?? null },
      meta: meta(),
    });
  }),
  http.get("/api/v1/demo/scenarios", ({ request }) => {
    if (scenarioFrom(request) === "error") {
      return demoFailure(503, "DEMO_UNAVAILABLE", "Demo scenarios are unavailable.", true);
    }
    return HttpResponse.json({ data: demoScenarioDefinitions, meta: meta() });
  }),
  http.get("/api/v1/demo/runs", ({ request }) => {
    if (scenarioFrom(request) === "error") {
      return demoFailure(503, "DEMO_UNAVAILABLE", "Demo Run history is unavailable.", true);
    }
    const url = new URL(request.url);
    const offset = Number(url.searchParams.get("cursor") ?? "0") || 0;
    const limit = Math.min(100, Number(url.searchParams.get("limit") ?? "10") || 10);
    const items = mockDemoRuns.slice(offset, offset + limit);
    const nextCursor = offset + limit < mockDemoRuns.length ? String(offset + limit) : null;
    return HttpResponse.json({
      data: { items, nextCursor, total: mockDemoRuns.length },
      meta: meta(),
    });
  }),
  http.post("/api/v1/demo/runs", async ({ request }) => {
    const state = scenarioFrom(request);
    if (state === "partial") {
      return demoFailure(
        409,
        "DEMO_NOT_READY",
        "Required Demo Lab components are not ready.",
        true,
        demoStatus.components.filter(
          (component) => component.required && component.status !== "healthy",
        ),
      );
    }
    const requestId = request.headers.get("X-Request-ID")?.trim();
    if (!requestId) {
      return demoFailure(400, "INVALID_REQUEST", "X-Request-ID is required.");
    }
    const existingRunId = mockDemoRequests.get(requestId);
    if (existingRunId) {
      const existing = mockDemoRuns.find((run) => run.runId === existingRunId);
      if (existing) return demoResponse(existing, 202);
    }
    if (mockDemoActiveRun()) {
      return demoFailure(409, "DEMO_RUN_BUSY", "Another Demo Run is active.");
    }
    const input = (await request.json()) as DemoCreateRunRequest;
    if (
      !demoScenarioDefinitions.some((definition) => definition.id === input.scenario) ||
      (input.delayMs !== undefined && (input.delayMs < 0 || input.delayMs > 2_000))
    ) {
      return demoFailure(400, "INVALID_REQUEST", "The Demo Lab request is invalid.");
    }
    const run = resetMockDemoRun(input.scenario);
    run.delayMs = input.delayMs ?? 700;
    mockDemoRequests.set(requestId, run.runId);
    return demoResponse(run, 202);
  }),
  http.get("/api/v1/demo/runs/:runId/events", ({ params }) => {
    const run = mockDemoRuns.find((item) => item.runId === String(params.runId));
    if (!run) return demoFailure(404, "NOT_FOUND", "Demo Run was not found.");
    return new HttpResponse(
      `id: ${run.runVersion}\nevent: snapshot\ndata: ${JSON.stringify(run)}\n\n`,
      {
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache",
        },
      },
    );
  }),
  http.get("/api/v1/demo/runs/:runId", ({ params }) => {
    const run = mockDemoRuns.find((item) => item.runId === String(params.runId));
    return run ? demoResponse(run) : demoFailure(404, "NOT_FOUND", "Demo Run was not found.");
  }),
  http.post("/api/v1/demo/runs/:runId/cancel", async ({ request, params }) => {
    const input = (await request.json()) as { confirm: boolean; note: string };
    if (!input.confirm || !input.note.trim()) {
      return demoFailure(400, "INVALID_REQUEST", "Confirmation and an operator note are required.");
    }
    const run = mockDemoRuns.find((item) => item.runId === String(params.runId));
    if (!run) return demoFailure(404, "NOT_FOUND", "Demo Run was not found.");
    if (!activeDemoStatuses.has(run.status)) {
      return demoFailure(409, "DEMO_RUN_STATE_CHANGED", "The Demo Run is already terminal.");
    }
    const completedAt = new Date().toISOString();
    run.status = "cancelled";
    run.outcome = "cancelled";
    run.completedAt = completedAt;
    run.lastHeartbeatAt = completedAt;
    run.runVersion += 1;
    return demoResponse(run);
  }),
  http.get("/api/v1/protect/approvals", ({ request }) =>
    pageResponse(request, mockProtectApprovals, "agentguard"),
  ),
  http.post("/api/v1/protect/approvals/:ticketId/:decision", async ({ request, params }) => {
    const input = (await request.json()) as ConfirmedActionRequest;
    await delay(120);
    if (!input.confirmed || !input.note.trim()) {
      return protectFailure(
        400,
        "INVALID_REQUEST",
        "Confirmation and an operator note are required.",
      );
    }
    const ticketId = String(params.ticketId);
    const decision = String(params.decision);
    if (decision !== "approve" && decision !== "deny") {
      return protectFailure(404, "NOT_FOUND", "The approval action is not available.");
    }
    if (ticketId === "ticket-expired") {
      mockProtectApprovals = mockProtectApprovals.filter((item) => item.id !== ticketId);
      return protectFailure(404, "NOT_FOUND", "The ticket is no longer pending.");
    }
    if (scenarioFrom(request) === "partial") {
      const key = `${ticketId}:${decision}`;
      const attempt = (approvalAttempts.get(key) ?? 0) + 1;
      approvalAttempts.set(key, attempt);
      if (attempt === 1) {
        return protectFailure(
          503,
          "UPSTREAM_UNAVAILABLE",
          "AgentGuard timed out. Confirm the ticket state before retrying.",
          true,
        );
      }
    }
    const index = mockProtectApprovals.findIndex((item) => item.id === ticketId);
    if (index < 0) return protectFailure(404, "NOT_FOUND", "The ticket is no longer pending.");
    mockProtectApprovals.splice(index, 1);
    resolveMockDemoApproval(ticketId, decision);
    return protectReceipt(
      `${decision}-approval`,
      ticketId,
      decision === "approve" ? "Approval ticket approved" : "Approval ticket denied",
    );
  }),
  http.get("/api/v1/audit/traces", ({ request }) => traceListResponse(request)),
  http.get("/api/v1/audit/traces/:traceId", async ({ request, params }) => {
    const scenario = scenarioFrom(request);
    if (scenario === "loading") await delay(30_000);
    if (scenario === "error") {
      return traceFailure(503, "DATABASE_UNAVAILABLE", "Trace storage is temporarily unavailable.");
    }
    const traceId = String(params.traceId);
    if (traceId === "ffffffffffffffffffffffffffffffff") {
      return traceFailure(
        403,
        "FORBIDDEN",
        "This administrator cannot read retained Trace detail.",
      );
    }
    const detail = scenario === "empty" ? undefined : mockTraceDetails[traceId];
    if (!detail) return traceFailure(404, "NOT_FOUND", "The Trace was not found.");
    return HttpResponse.json({ data: detail, meta: traceMeta() });
  }),
  http.get("/api/v1/audit/traces/:traceId/spans/:spanId", async ({ request, params }) => {
    const scenario = scenarioFrom(request);
    if (scenario === "loading") await delay(30_000);
    if (scenario === "error") {
      return traceFailure(
        503,
        "DATABASE_UNAVAILABLE",
        "Trace payload storage is temporarily unavailable.",
      );
    }
    const traceId = String(params.traceId);
    const spanId = String(params.spanId);
    if (traceId === "ffffffffffffffffffffffffffffffff") {
      return traceFailure(403, "FORBIDDEN", "This administrator cannot read retained content.");
    }
    const detail = scenario === "empty" ? undefined : mockTraceSpanDetails[`${traceId}:${spanId}`];
    if (!detail) return traceFailure(404, "NOT_FOUND", "The Span was not found in this Trace.");
    return HttpResponse.json({ data: detail, meta: traceMeta() });
  }),
  http.get("/api/v1/audit/analytics", ({ request }) => respond(request, auditData, emptyAudit)),
  http.get("/api/v1/audit/events", ({ request }) =>
    listResponse(request, auditData.events, "agentgateway"),
  ),
  http.get("/api/v1/audit/events/:source/:eventId", ({ request, params }) => {
    const event = auditData.events.find(
      (item) => item.source === params.source && item.id === params.eventId,
    );
    if (!event) {
      return HttpResponse.json(
        {
          error: {
            code: "NOT_FOUND",
            message: "Event is outside the bounded mock activity buffer",
            requestId: "req_mock_event_404",
            retryable: false,
          },
        } satisfies ApiFailure,
        { status: 404 },
      );
    }
    const detail = completeAuditEvent(event);
    return respond(request, detail, detail, event.source);
  }),
  http.get("/api/v1/audit/sessions", ({ request }) =>
    listResponse(request, auditData.sessions, "agentguard"),
  ),
  http.get("/api/v1/stream", ({ request }) => {
    const scenario = scenarioFrom(request);
    if (scenario === "error") {
      return failure();
    }

    let index = 0;
    let sequence = 0;
    let timer: ReturnType<typeof setInterval> | undefined;
    const encoder = new TextEncoder();
    const stream = new ReadableStream({
      start(controller) {
        const emit = () => {
          const fixture = baseEvents[index % baseEvents.length];
          const event: UnifiedEvent = {
            ...fixture,
            id: `mock-live-${index}-${fixture.id}`,
            timestamp: new Date().toISOString(),
            summary: `[Mock live] ${fixture.summary}`,
          };
          controller.enqueue(
            encoder.encode(
              `id: ${++sequence}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`,
            ),
          );
          if (index % 2 === 1) {
            const trace = advanceRunningTrace();
            if (trace) {
              controller.enqueue(
                encoder.encode(
                  `id: ${++sequence}\nevent: trace\ndata: ${JSON.stringify(trace)}\n\n`,
                ),
              );
            }
          }
          index += 1;
        };
        controller.enqueue(encoder.encode(": mock heartbeat\n\n"));
        emit();
        timer = setInterval(emit, 2_000);
        request.signal.addEventListener("abort", () => {
          if (timer) clearInterval(timer);
          controller.close();
        });
      },
      cancel() {
        if (timer) clearInterval(timer);
      },
    });

    return new HttpResponse(stream, {
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      },
    });
  }),
];
