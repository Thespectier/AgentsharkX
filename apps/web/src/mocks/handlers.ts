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
  LabelUpdate,
  MCPDetectionRequest,
  ProtectSnapshot,
  RuntimeRule,
  RuntimeRuleCheckRequest,
  RuntimeRulePublishRequest,
  SkillDetectionRequest,
  TrustResource,
  TrustScanJob,
} from "../generated/api-client";
import {
  auditData,
  baseEvents,
  connectData,
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
};

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
const mockScanJobs = new Map(trustScans.map((job) => [job.id, structuredClone(job)]));
const mockScanPolls = new Map<string, number>();
const mockScanAttempts = new Map<string, number>();
let nextScan = 300;

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

export const handlers = [
  http.get("/api/v1/overview", ({ request }) => respond(request, overviewData, emptyOverview)),
  http.get("/api/v1/system/health", ({ request }) =>
    listResponse(request, overviewData.health, "agentgateway"),
  ),
  http.get("/api/v1/system/capabilities", ({ request }) =>
    listResponse(request, capabilityData, "agentgateway"),
  ),
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
  http.get("/api/v1/connect/mcp/servers", ({ request }) =>
    pageResponse(request, connectData.mcpServers, "agentgateway"),
  ),
  http.get("/api/v1/connect/mcp/servers/:resourceId", ({ request, params }) => {
    const item = connectData.mcpServers.find((server) => server.id === params.resourceId);
    return item ? respond(request, item, item, "agentgateway") : failure("agentgateway");
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
    return protectReceipt(
      `${decision}-approval`,
      ticketId,
      decision === "approve" ? "Approval ticket approved" : "Approval ticket denied",
    );
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
    return respond(request, event, event, event.source);
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
              `id: ${event.id}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`,
            ),
          );
          index += 1;
        };
        controller.enqueue(encoder.encode(": mock heartbeat\n\n"));
        timer = setInterval(emit, 3_600);
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
