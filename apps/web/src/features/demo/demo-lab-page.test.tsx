import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { DemoRun, DemoStatus } from "../../generated/api-client";
import { demoRuns, demoScenarioDefinitions, demoStatus } from "../../mocks/data";
import { server } from "../../mocks/server";
import { DemoLabPage } from "./demo-lab-page";

const meta = { fetchedAt: "2026-07-30T08:00:00Z", stale: false };

class QuietEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;

  readyState = QuietEventSource.CONNECTING;
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(_url: string | URL) {}
  addEventListener() {}
  close() {
    this.readyState = QuietEventSource.CLOSED;
  }
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={client}>
      <DemoLabPage />
    </QueryClientProvider>,
  );
}

function statusHandler(status: DemoStatus) {
  return http.get("/api/v1/demo/status", () => HttpResponse.json({ data: status, meta }));
}

function historyHandler(runs: DemoRun[]) {
  return http.get("/api/v1/demo/runs", () =>
    HttpResponse.json({
      data: { items: runs, nextCursor: null, total: runs.length },
      meta,
    }),
  );
}

function runHandler(run: DemoRun) {
  return http.get("/api/v1/demo/runs/:runId", () => HttpResponse.json({ data: run, meta }));
}

beforeEach(() => vi.stubGlobal("EventSource", QuietEventSource));
afterEach(() => vi.unstubAllGlobals());

describe("DemoLabPage", () => {
  it("renders a controlled page without writable controls when Demo Lab is disabled", async () => {
    server.use(
      statusHandler({
        ...demoStatus,
        enabled: false,
        ready: false,
        activeRunId: null,
        components: demoStatus.components.map((component) => ({
          ...component,
          status: "unknown",
          message: "Demo Lab is disabled",
        })),
      }),
    );

    renderPage();

    expect(await screen.findByRole("heading", { name: "Demo Lab is disabled" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Start Run" })).not.toBeInTheDocument();
  });

  it("switches the shared Trace from Flow to Timeline and opens Span detail", async () => {
    const user = userEvent.setup();
    const view = renderPage();

    expect(await screen.findByRole("group", { name: "Trace flow lanes" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Timeline" }));

    expect(screen.getByRole("region", { name: "Trace timeline" })).toBeVisible();
    expect(view.container.querySelector(".trace-flow")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Open span Plan research" }));
    expect(await screen.findByRole("dialog", { name: "Plan research" })).toBeVisible();
  });

  it("keeps unobserved metrics pending while a Run is active", async () => {
    const pendingRun: DemoRun = {
      ...structuredClone(demoRuns[0]),
      status: "running",
      outcome: "none",
      completedAt: null,
      traceId: undefined,
      rootSpanId: undefined,
      currentStep: "plan",
      completedSteps: 1,
      observedMetrics: null,
      correlations: {
        ...demoRuns[0].correlations,
        trace: { status: "pending", basis: "Waiting for matching task_id and session_id" },
      },
      links: { audit: "/audit/traces" },
    };
    server.use(
      statusHandler({ ...demoStatus, activeRunId: pendingRun.runId }),
      historyHandler([pendingRun]),
      runHandler(pendingRun),
    );

    renderPage();

    expect(await screen.findByText("plan")).toBeVisible();
    expect(screen.getAllByText("Pending").length).toBeGreaterThanOrEqual(8);
    expect(screen.getByRole("button", { name: "Start Run" })).toBeDisabled();
  });

  it("reuses the same request ID for an explicit Start retry", async () => {
    let attempts = 0;
    const requestIds: string[] = [];
    const bodies: unknown[] = [];
    server.use(
      statusHandler({ ...demoStatus, activeRunId: null }),
      historyHandler([]),
      http.get("/api/v1/demo/scenarios", () =>
        HttpResponse.json({ data: demoScenarioDefinitions, meta }),
      ),
      http.post("/api/v1/demo/runs", async ({ request }) => {
        attempts += 1;
        requestIds.push(request.headers.get("X-Request-ID") ?? "");
        bodies.push(await request.json());
        if (attempts === 1) {
          return HttpResponse.json(
            {
              error: {
                code: "DEMO_RUNNER_UNAVAILABLE",
                message: "Demo Runner is unavailable.",
                requestId: "req_demo_retry",
                retryable: true,
              },
            },
            { status: 503 },
          );
        }
        return HttpResponse.json({ data: demoRuns[0], meta }, { status: 202 });
      }),
    );
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole("button", { name: "Start Run" }));
    expect(await screen.findByRole("button", { name: "Retry start" })).toBeEnabled();
    expect(attempts).toBe(1);

    await user.click(screen.getByRole("button", { name: "Retry start" }));
    await waitFor(() => expect(attempts).toBe(2));
    expect(requestIds[0]).toMatch(/^demo-/);
    expect(requestIds[1]).toBe(requestIds[0]);
    expect(bodies).toEqual([
      { scenario: "approval", delayMs: 700 },
      { scenario: "approval", delayMs: 700 },
    ]);
  });

  it("opens the shared Protect decision dialog for an exact-session approval", async () => {
    const approvalRun: DemoRun = {
      ...structuredClone(demoRuns[2]),
      status: "waiting_approval",
      outcome: "none",
      completedAt: null,
      currentStep: "guarded_action",
      completedSteps: 7,
      approval: { ...demoRuns[2].approval!, status: "pending" },
    };
    server.use(
      statusHandler({ ...demoStatus, activeRunId: approvalRun.runId }),
      historyHandler([approvalRun]),
      runHandler(approvalRun),
    );
    const user = userEvent.setup();
    renderPage();

    expect(await screen.findByText("Linked by exact session_id")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Review decision" }));

    expect(screen.getByRole("dialog", { name: "Review send_http" })).toBeVisible();
    expect(screen.getByLabelText("Operator note")).toBeVisible();
    expect(screen.getByRole("button", { name: "Approve" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Deny" })).toBeDisabled();
  });

  it.each([
    ["failed", "Run failed"],
    ["interrupted", "Run interrupted"],
    ["expired", "Run expired"],
    ["cancelled", "Run cancelled"],
  ] as const)("renders %s as its own terminal state", async (status, label) => {
    const terminalRun: DemoRun = {
      ...structuredClone(demoRuns[0]),
      status,
      outcome: status === "cancelled" ? "cancelled" : "failed",
      errorCode: undefined,
      errorSummary: undefined,
    };
    server.use(
      statusHandler({ ...demoStatus, activeRunId: null }),
      historyHandler([terminalRun]),
      runHandler(terminalRun),
    );

    renderPage();

    expect(await screen.findByText(label)).toBeVisible();
  });
});
