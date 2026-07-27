import { describe, expect, it } from "vitest";

import { baseEvents } from "../../mocks/data";
import type { UnifiedEvent } from "../../types";
import {
  filterAuditEvents,
  gatewayLogHref,
  sensitiveContentRows,
  sourceEvidenceRows,
} from "./audit-page";

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

  it("reports upstream payload retention without exposing sensitive content", () => {
    expect(sensitiveContentRows(gatewayEvent)).toEqual([
      { label: "Prompt", value: "Not collected by AgentsharkX" },
      { label: "Payload", value: "Retained upstream; content not retrieved" },
      { label: "Authorization", value: "Credential values are never collected" },
      { label: "Tool arguments", value: "Not collected by AgentsharkX" },
    ]);
    expect(JSON.stringify(sourceEvidenceRows(gatewayEvent))).not.toContain("authorization");
  });

  it("builds an exact native Logs link from the verified upstream log ID", () => {
    expect(gatewayLogHref("http://localhost:15000/ui/llm/logs", gatewayEvent)).toBe(
      "http://localhost:15000/ui/llm/logs?log=log-a",
    );
    expect(
      gatewayLogHref("http://localhost:15000/ui/llm/logs", {
        ...gatewayEvent,
        rawRef: { source: "agentgateway", id: "log/a?b" },
      }),
    ).toBe("http://localhost:15000/ui/llm/logs?log=log%2Fa%3Fb");
  });

  it("does not create a source link for another source or an unsafe base URL", () => {
    expect(
      gatewayLogHref("http://localhost:15000/ui/llm/logs", {
        ...gatewayEvent,
        source: "agentguard",
        rawRef: { source: "agentguard", id: "audit-a" },
      }),
    ).toBeUndefined();
    expect(gatewayLogHref("javascript:alert(1)", gatewayEvent)).toBeUndefined();
  });
});
