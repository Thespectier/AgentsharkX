import { describe, expect, it } from "vitest";

import {
  changeGuardKind,
  emptyGuardrailValue,
  targetForKind,
  validateGuardrailValue,
} from "./gateway-guardrail-model";

describe("gateway guardrail model", () => {
  it("accepts complete verified LLM guardrails", () => {
    expect(
      validateGuardrailValue("llm", {
        streaming: "Enabled",
        request: [
          {
            regex: {
              action: "mask",
              rules: [{ builtin: "email" }, { pattern: "secret-[a-z]+" }],
            },
            rejection: {
              status: 422,
              body: "rejected",
              headers: { set: { "x-result": "rejected" } },
            },
          },
          {
            webhook: {
              target: { service: { name: "default/guardrail", port: 8080 } },
              failureMode: "failOpen",
              forwardHeaderMatches: [{ name: "x-tenant", value: { regex: ".+" } }],
            },
          },
          { openAIModeration: { model: "omni-moderation-latest", policies: {} } },
          {
            bedrockGuardrails: {
              guardrailIdentifier: "guardrail",
              guardrailVersion: "1",
              region: "us-west-2",
            },
          },
          { googleModelArmor: { templateId: "template", projectId: "project" } },
          { azureContentSafety: { endpoint: "safety.example.invalid" } },
        ],
        response: [
          {
            webhook: {
              target: { backend: "guardrail-backend" },
              failureMode: "failClosed",
            },
          },
        ],
      }),
    ).toBeNull();
  });

  it("rejects OpenAI moderation in the response phase", () => {
    expect(
      validateGuardrailValue("llm", {
        response: [{ openAIModeration: {} }],
      }),
    ).toBe("OpenAI moderation is available only for request guards.");
  });

  it("rejects invalid verified provider fields before an upstream write", () => {
    expect(
      validateGuardrailValue("llm", {
        request: [
          {
            azureContentSafety: {
              endpoint: "safety.example.invalid",
              analyzeText: { severityThreshold: 99 },
            },
          },
        ],
      }),
    ).toBe("request guard 1 Azure severity threshold must be an integer from 0 to 6.");
    expect(
      validateGuardrailValue("llm", {
        request: [
          {
            webhook: {
              target: { host: "guardrail.example.invalid:9000" },
              forwardHeaderMatches: [{ name: "x-tenant", value: { prefix: "tenant-" } }],
            },
          },
        ],
      }),
    ).toBe("request guard 1 forwarded header match 1 needs exactly one exact or regex value.");
    expect(
      validateGuardrailValue("llm", {
        response: [
          {
            azureContentSafety: {
              endpoint: "safety.example.invalid",
              detectJailbreak: {},
            },
          },
        ],
      }),
    ).toBe("Azure jailbreak detection is available only for request guards.");
  });

  it("accepts complete ordered MCP processors", () => {
    expect(
      validateGuardrailValue("mcp", {
        processors: [
          {
            kind: "remote",
            host: "guardrail.example.invalid:9000",
            failureMode: "failOpen",
            methods: {
              "tools/call": "full",
              "tools/*": "request",
              "*/list": "response",
              "*": "off",
            },
            metadata: { tenant: "jwt.sub" },
            requestHeaders: { allowed: ["authorization"], disallowed: ["cookie"] },
            policies: { requestHeaderModifier: { set: { "x-source": "mcp" } } },
          },
        ],
      }),
    ).toBeNull();
  });

  it("preserves sibling fields when changing a guard kind", () => {
    const changed = changeGuardKind(
      {
        regex: { action: "reject", rules: [{ pattern: "secret" }] },
        rejection: { status: 451, headers: { set: { "x-denied": "true" } } },
      },
      "webhook",
    );
    expect(changed).toEqual({
      webhook: { target: { host: "" }, failureMode: "failClosed" },
      rejection: { status: 451, headers: { set: { "x-denied": "true" } } },
    });
  });

  it("creates exact empty values and all target variants", () => {
    expect(emptyGuardrailValue("llm")).toEqual({
      streaming: "Disabled",
      request: [],
      response: [],
    });
    expect(targetForKind("host")).toEqual({ host: "" });
    expect(targetForKind("backend")).toEqual({ backend: "" });
    expect(targetForKind("service")).toEqual({ service: { name: "", port: 0 } });
  });

  it("rejects URL hosts and non-namespaced service references", () => {
    expect(
      validateGuardrailValue("llm", {
        request: [{ webhook: { target: { host: "https://guardrail.example.invalid" } } }],
      }),
    ).toBe("request guard 1 webhook host must use connection host:port or unix:/path format.");
    expect(
      validateGuardrailValue("mcp", {
        processors: [
          {
            kind: "remote",
            service: { name: "guardrail.default.svc", port: 9000 },
            methods: { "tools/call": "full" },
          },
        ],
      }),
    ).toBe("MCP processor 1 service name must use namespace/hostname format.");
  });
});
