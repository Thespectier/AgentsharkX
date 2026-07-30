import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { demoRuns } from "../mocks/data";
import { server } from "../mocks/server";
import type { ApiFailure, Envelope, OverviewData } from "../types";
import { getScenario, mutateOperation, withScenario } from "./api";

describe("scenario-aware mock API", () => {
  it("defaults to the normal demo and preserves explicit failure states", () => {
    expect(getScenario()).toBe("normal");
    window.history.replaceState({}, "", "/audit/analytics?scenario=partial");
    expect(getScenario()).toBe("partial");
    expect(withScenario("/api/v1/overview")).toBe("/api/v1/overview?scenario=partial");
  });

  it("returns source-labelled partial data", async () => {
    const response = await fetch(
      new URL("/api/v1/overview?scenario=partial", window.location.origin),
    );
    const body = (await response.json()) as Envelope<OverviewData>;

    expect(response.status).toBe(200);
    expect(body.meta.partial).toBe(true);
    expect(body.meta.sourceFailures).toEqual([
      expect.objectContaining({ source: "agentguard", code: "UPSTREAM_TIMEOUT" }),
    ]);
    expect(body.data.health.map((item) => item.source)).toEqual(["agentgateway", "agentguard"]);
  });

  it("returns bounded empty data and a structured total failure", async () => {
    const emptyResponse = await fetch(
      new URL("/api/v1/audit/events?scenario=empty", window.location.origin),
    );
    const emptyBody = (await emptyResponse.json()) as Envelope<unknown[]>;
    expect(emptyBody.data).toEqual([]);

    const errorResponse = await fetch(
      new URL("/api/v1/overview?scenario=error", window.location.origin),
    );
    const errorBody = (await errorResponse.json()) as ApiFailure;
    expect(errorResponse.status).toBe(503);
    expect(errorBody.error).toMatchObject({
      code: "UPSTREAM_UNAVAILABLE",
      retryable: true,
    });
  });

  it("forwards an explicit request ID on write operations", async () => {
    let requestId = "";
    server.use(
      http.post("/api/v1/demo/runs", ({ request }) => {
        requestId = request.headers.get("X-Request-ID") ?? "";
        return HttpResponse.json(
          {
            data: demoRuns[0],
            meta: { fetchedAt: "2026-07-30T08:00:00Z", stale: false },
          },
          { status: 202 },
        );
      }),
    );

    await mutateOperation(
      "createDemoRun",
      { scenario: "happy", delayMs: 0 },
      { requestId: "demo-stable-request" },
    );

    expect(requestId).toBe("demo-stable-request");
  });
});
