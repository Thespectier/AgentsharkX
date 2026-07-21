import { delay, http, HttpResponse } from "msw";

import type {
  ApiFailure,
  AuditData,
  ConnectData,
  Envelope,
  OverviewData,
  ProtectData,
  ResponseMeta,
  Scenario,
  Source,
  TrustData,
  UnifiedEvent,
} from "../types";
import { auditData, baseEvents, connectData, overviewData, protectData, trustData } from "./data";

const capturedAt = "2026-07-21T12:42:10Z";

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

const emptyConnect: ConnectData = {
  ...connectData,
  summary: connectData.summary.map((item) => ({ ...item, value: 0, healthy: 0 })),
  routes: [],
  providers: [],
  models: [],
  mcpServers: [],
};

const emptyTrust: TrustData = {
  agents: [],
  resources: [],
  trustDistribution: [],
  scans: [],
};

const emptyProtect: ProtectData = {
  policies: [],
  approvals: [],
  coverage: protectData.coverage.map((item) => ({ ...item, active: 0 })),
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

export const handlers = [
  http.get("/api/v1/overview", ({ request }) => respond(request, overviewData, emptyOverview)),
  http.get("/api/v1/system/health", ({ request }) =>
    listResponse(request, overviewData.health, "agentgateway"),
  ),
  http.get("/api/v1/connect/summary", ({ request }) =>
    respond(request, connectData, emptyConnect, "agentgateway"),
  ),
  http.get("/api/v1/connect/llm/providers", ({ request }) =>
    listResponse(request, connectData.providers, "agentgateway"),
  ),
  http.get("/api/v1/connect/llm/models", ({ request }) =>
    listResponse(request, connectData.models, "agentgateway"),
  ),
  http.get("/api/v1/connect/mcp/servers", ({ request }) =>
    listResponse(request, connectData.mcpServers, "agentgateway"),
  ),
  http.get("/api/v1/connect/traffic/routes", ({ request }) =>
    listResponse(request, connectData.routes, "agentgateway"),
  ),
  http.get("/api/v1/trust/agents", ({ request }) =>
    listResponse(request, trustData.agents, "agentguard"),
  ),
  http.get("/api/v1/trust/agents/:agentId", ({ request, params }) => {
    const agent = trustData.agents.find((item) => item.id === params.agentId);
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
    return respond(request, agent, agent, "agentguard");
  }),
  http.get("/api/v1/trust/resources", ({ request }) =>
    respond(request, trustData, emptyTrust, "agentguard"),
  ),
  http.get("/api/v1/protect/policies", ({ request }) =>
    respond(request, protectData, emptyProtect),
  ),
  http.get("/api/v1/protect/approvals", ({ request }) =>
    listResponse(request, protectData.approvals, "agentguard"),
  ),
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
