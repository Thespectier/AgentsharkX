import { describe, expect, it } from "vitest";

import { baseEvents } from "../../mocks/data";
import type { UnifiedEvent } from "../../types";
import { filterAuditEvents, gatewayPayloadSections, sourceEvidenceRows } from "./audit-page";

describe("audit filters", () => {
  it("filters by verified source, severity, and displayed event fields", () => {
    expect(
      filterAuditEvents(baseEvents, {
        source: "agentguard",
        severity: "critical",
        query: "shell invocation",
      }).map((event) => event.id),
    ).toEqual(["guard-audit-9013"]);
  });

  it("returns every event when filters are reset", () => {
    expect(
      filterAuditEvents(baseEvents, { source: "all", severity: "all", query: "" }),
    ).toHaveLength(baseEvents.length);
  });

  it("applies a case-sensitive exact session identifier from a Demo deep link", () => {
    const filters = { source: "all" as const, severity: "all" as const, query: "" };
    const expected = baseEvents
      .filter(
        (event) =>
          event.subject?.sessionId === "ses_rg_84f2" ||
          event.correlation?.sessionId === "ses_rg_84f2",
      )
      .map((event) => event.id);

    expect(filterAuditEvents(baseEvents, filters, "ses_rg_84f2").map((event) => event.id)).toEqual(
      expected,
    );
    expect(filterAuditEvents(baseEvents, filters, "SES_RG_84F2")).toEqual([]);
  });
});

describe("audit event detail", () => {
  const gatewayEvent: UnifiedEvent = {
    id: "gateway:log-a",
    timestamp: "2026-07-24T08:00:01Z",
    source: "agentgateway",
    kind: "traffic",
    severity: "info",
    target: { provider: "deepseek", model: "deepseek-chat" },
    summary: "request completed",
    rawRef: { source: "agentgateway", id: "log-a" },
    raw: {
      durationMs: 321,
      httpStatus: 200,
      hasPayload: true,
      traceId: "trace-a",
      genAi: {
        operationName: "chat",
        providerName: "deepseek",
        requestModel: "deepseek-chat",
        responseModel: "deepseek-chat",
      },
      usage: { inputTokens: 12, outputTokens: 5, totalTokens: 17 },
      attributes: {
        "request.authorization": "Bearer complete-test-value",
        nested: { retained: true },
      },
      payload: {
        requestPrompt: [{ role: "user", content: "complete prompt" }],
        responseCompletion: { choices: [{ message: { content: "complete response" } }] },
      },
    },
  };

  it("surfaces the verified allow-listed gateway evidence", () => {
    expect(sourceEvidenceRows(gatewayEvent)).toEqual(
      expect.arrayContaining([
        { label: "Duration", value: "321 ms" },
        { label: "HTTP status", value: "200" },
        { label: "Provider", value: "deepseek" },
        { label: "Total tokens", value: "17" },
        { label: "Trace ID", value: "trace-a" },
      ]),
    );
  });

  it("returns every complete payload section supplied by the source detail", () => {
    expect(gatewayPayloadSections(gatewayEvent)).toEqual([
      {
        label: "Request prompt",
        value: [{ role: "user", content: "complete prompt" }],
      },
      {
        label: "Response completion",
        value: { choices: [{ message: { content: "complete response" } }] },
      },
      {
        label: "Attributes",
        value: {
          "request.authorization": "Bearer complete-test-value",
          nested: { retained: true },
        },
      },
    ]);
  });
});
