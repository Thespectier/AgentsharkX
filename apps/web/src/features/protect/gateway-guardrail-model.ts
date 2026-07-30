export type JsonRecord = Record<string, unknown>;
export type GuardrailFamily = "llm" | "mcp";
export type GuardrailPhase = "request" | "response";
export type LlmGuardKind =
  | "regex"
  | "webhook"
  | "openAIModeration"
  | "bedrockGuardrails"
  | "googleModelArmor"
  | "azureContentSafety";

export const llmRequestGuardKinds: LlmGuardKind[] = [
  "regex",
  "webhook",
  "openAIModeration",
  "bedrockGuardrails",
  "googleModelArmor",
  "azureContentSafety",
];

export const llmResponseGuardKinds = llmRequestGuardKinds.filter(
  (kind): kind is Exclude<LlmGuardKind, "openAIModeration"> => kind !== "openAIModeration",
);

export const builtinRules = ["ssn", "creditCard", "phoneNumber", "email", "caSin"] as const;
export const mcpPhases = ["off", "request", "response", "full"] as const;

export function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

export function objectValue(value: unknown): JsonRecord {
  return isRecord(value) ? value : {};
}

export function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

export function jsonText(value: unknown) {
  if (value === undefined) return "";
  return JSON.stringify(value, null, 2) ?? "";
}

export function moveItem<T>(items: T[], index: number, direction: -1 | 1) {
  const target = index + direction;
  if (target < 0 || target >= items.length) return items;
  const next = [...items];
  [next[index], next[target]] = [next[target], next[index]];
  return next;
}

export function emptyGuardrailValue(family: GuardrailFamily): JsonRecord {
  return family === "llm"
    ? { streaming: "Disabled", request: [], response: [] }
    : {
        processors: [
          {
            kind: "remote",
            host: "",
            failureMode: "failClosed",
            methods: { "tools/call": "request" },
          },
        ],
      };
}

export function guardKind(value: unknown): LlmGuardKind | "unsupported" {
  const guard = objectValue(value);
  return llmRequestGuardKinds.find((kind) => isRecord(guard[kind])) ?? "unsupported";
}

export function emptyLlmGuard(kind: LlmGuardKind): JsonRecord {
  switch (kind) {
    case "regex":
      return { regex: { action: "mask", rules: [{ builtin: "email" }] } };
    case "webhook":
      return {
        webhook: {
          target: { host: "" },
          failureMode: "failClosed",
        },
      };
    case "openAIModeration":
      return { openAIModeration: {} };
    case "bedrockGuardrails":
      return {
        bedrockGuardrails: {
          guardrailIdentifier: "",
          guardrailVersion: "",
          region: "",
        },
      };
    case "googleModelArmor":
      return { googleModelArmor: { templateId: "", projectId: "" } };
    case "azureContentSafety":
      return { azureContentSafety: { endpoint: "" } };
  }
}

export function changeGuardKind(value: unknown, kind: LlmGuardKind): JsonRecord {
  const current = objectValue(value);
  const next = { ...current };
  for (const candidate of llmRequestGuardKinds) delete next[candidate];
  return { ...next, ...emptyLlmGuard(kind) };
}

export function targetKind(value: unknown): "host" | "backend" | "service" {
  const target = objectValue(value);
  if (isRecord(target.service)) return "service";
  if (typeof target.backend === "string") return "backend";
  return "host";
}

export function targetForKind(
  kind: "host" | "backend" | "service",
  previous?: unknown,
): JsonRecord {
  const current = objectValue(previous);
  if (kind === "service") {
    const service = objectValue(current.service);
    return {
      service: {
        name: typeof service.name === "string" ? service.name : "",
        port: typeof service.port === "number" ? service.port : 0,
      },
    };
  }
  if (kind === "backend") {
    return { backend: typeof current.backend === "string" ? current.backend : "" };
  }
  return { host: typeof current.host === "string" ? current.host : "" };
}

export function validateGuardrailValue(family: GuardrailFamily, value: unknown): string | null {
  if (!isRecord(value)) return "Guardrail value must be a JSON object.";
  return family === "llm" ? validateLlmGuardrails(value) : validateMcpGuardrails(value);
}

function validateLlmGuardrails(value: JsonRecord): string | null {
  if (
    value.streaming !== undefined &&
    value.streaming !== "Disabled" &&
    value.streaming !== "Enabled"
  ) {
    return "LLM streaming must be Disabled or Enabled.";
  }
  for (const phase of ["request", "response"] as const) {
    const guards = value[phase];
    if (guards !== undefined && !Array.isArray(guards)) {
      return `LLM ${phase} guards must be an array.`;
    }
    for (const [index, candidate] of arrayValue(guards).entries()) {
      const prefix = `${phase} guard ${index + 1}`;
      if (!isRecord(candidate)) return `${prefix} must be a JSON object.`;
      const variants = llmRequestGuardKinds.filter((kind) => candidate[kind] !== undefined);
      if (variants.length !== 1) return `${prefix} must contain exactly one guard type.`;
      const kind = variants[0];
      if (phase === "response" && kind === "openAIModeration") {
        return "OpenAI moderation is available only for request guards.";
      }
      if (!isRecord(candidate[kind])) return `${prefix} ${kind} must be a JSON object.`;
      const variantError = validateLlmVariant(kind, candidate[kind] as JsonRecord, prefix, phase);
      if (variantError) return variantError;
      const rejectionError = validateRejection(candidate.rejection, prefix);
      if (rejectionError) return rejectionError;
    }
  }
  return null;
}

function validateLlmVariant(
  kind: LlmGuardKind,
  value: JsonRecord,
  prefix: string,
  phase: GuardrailPhase,
): string | null {
  if (kind === "regex") {
    if (value.action !== undefined && value.action !== "mask" && value.action !== "reject") {
      return `${prefix} regex action must be mask or reject.`;
    }
    if (!Array.isArray(value.rules) || value.rules.length === 0) {
      return `${prefix} regex needs at least one rule.`;
    }
    for (const rule of value.rules) {
      if (!isRecord(rule)) return `${prefix} regex rules must be JSON objects.`;
      const hasBuiltin = typeof rule.builtin === "string";
      const hasPattern = typeof rule.pattern === "string";
      if (hasBuiltin === hasPattern) {
        return `${prefix} regex rules need exactly one builtin or pattern.`;
      }
      if (hasBuiltin && !builtinRules.includes(rule.builtin as (typeof builtinRules)[number])) {
        return `${prefix} contains an unsupported built-in detector.`;
      }
      if (hasPattern && !(rule.pattern as string).trim()) {
        return `${prefix} contains an empty regex pattern.`;
      }
    }
  }
  if (kind === "webhook") {
    const targetError = validateTarget(value.target, `${prefix} webhook`);
    if (targetError) return targetError;
    if (
      value.failureMode !== undefined &&
      value.failureMode !== "failClosed" &&
      value.failureMode !== "failOpen"
    ) {
      return `${prefix} webhook failure mode must be failClosed or failOpen.`;
    }
    const headerError = validateHeaderMatches(value.forwardHeaderMatches, prefix);
    if (headerError) return headerError;
  }
  if (
    kind === "openAIModeration" &&
    value.model !== undefined &&
    value.model !== null &&
    typeof value.model !== "string"
  ) {
    return `${prefix} moderation model must be a string or null.`;
  }
  if (kind === "bedrockGuardrails") {
    for (const field of ["guardrailIdentifier", "guardrailVersion", "region"] as const) {
      if (typeof value[field] !== "string" || !value[field].trim()) {
        return `${prefix} Bedrock ${field} is required.`;
      }
    }
  }
  if (kind === "googleModelArmor") {
    for (const field of ["templateId", "projectId"] as const) {
      if (typeof value[field] !== "string" || !value[field].trim()) {
        return `${prefix} Google ${field} is required.`;
      }
    }
    if (
      value.location !== undefined &&
      value.location !== null &&
      typeof value.location !== "string"
    ) {
      return `${prefix} Google location must be a string or null.`;
    }
  }
  if (kind === "azureContentSafety") {
    if (typeof value.endpoint !== "string" || !value.endpoint.trim()) {
      return `${prefix} Azure endpoint is required.`;
    }
    const analyzeError = validateAzureAnalyzeText(value.analyzeText, prefix);
    if (analyzeError) return analyzeError;
    const jailbreakError = validateAzureJailbreak(value.detectJailbreak, prefix, phase);
    if (jailbreakError) return jailbreakError;
  }
  const policiesError = validateOptionalObject(value.policies, `${prefix} backend policies`);
  if (policiesError) return policiesError;
  return null;
}

function validateHeaderMatches(value: unknown, prefix: string): string | null {
  if (value === undefined) return null;
  if (!Array.isArray(value)) return `${prefix} forwarded header matches must be an array.`;
  for (const [index, candidate] of value.entries()) {
    if (!isRecord(candidate) || typeof candidate.name !== "string" || !candidate.name.trim()) {
      return `${prefix} forwarded header match ${index + 1} needs a header name.`;
    }
    if (candidate.value === "invalid") continue;
    const headerValue = candidate.value;
    if (!isRecord(headerValue)) {
      return `${prefix} forwarded header match ${index + 1} needs an exact or regex value.`;
    }
    const matches = ["exact", "regex"].filter((field) => typeof headerValue[field] === "string");
    if (matches.length !== 1 || Object.keys(headerValue).length !== 1) {
      return `${prefix} forwarded header match ${index + 1} needs exactly one exact or regex value.`;
    }
  }
  return null;
}

function validateAzureAnalyzeText(value: unknown, prefix: string): string | null {
  if (value === undefined || value === null) return null;
  if (!isRecord(value)) return `${prefix} Azure analyzeText must be a JSON object or null.`;
  if (
    value.severityThreshold !== undefined &&
    value.severityThreshold !== null &&
    (!Number.isInteger(value.severityThreshold) ||
      (value.severityThreshold as number) < 0 ||
      (value.severityThreshold as number) > 6)
  ) {
    return `${prefix} Azure severity threshold must be an integer from 0 to 6.`;
  }
  if (
    value.apiVersion !== undefined &&
    value.apiVersion !== null &&
    typeof value.apiVersion !== "string"
  ) {
    return `${prefix} Azure analyzeText API version must be a string or null.`;
  }
  if (
    value.blocklistNames !== undefined &&
    value.blocklistNames !== null &&
    (!Array.isArray(value.blocklistNames) ||
      value.blocklistNames.some((item) => typeof item !== "string"))
  ) {
    return `${prefix} Azure blocklist names must be a string array or null.`;
  }
  if (
    value.haltOnBlocklistHit !== undefined &&
    value.haltOnBlocklistHit !== null &&
    typeof value.haltOnBlocklistHit !== "boolean"
  ) {
    return `${prefix} Azure haltOnBlocklistHit must be a boolean or null.`;
  }
  return null;
}

function validateAzureJailbreak(
  value: unknown,
  prefix: string,
  phase: GuardrailPhase,
): string | null {
  if (value === undefined || value === null) return null;
  if (phase === "response")
    return "Azure jailbreak detection is available only for request guards.";
  if (!isRecord(value)) return `${prefix} Azure detectJailbreak must be a JSON object or null.`;
  if (
    value.apiVersion !== undefined &&
    value.apiVersion !== null &&
    typeof value.apiVersion !== "string"
  ) {
    return `${prefix} Azure jailbreak API version must be a string or null.`;
  }
  return null;
}

function validateRejection(value: unknown, prefix: string): string | null {
  if (value === undefined || value === null) return null;
  if (!isRecord(value)) return `${prefix} rejection must be a JSON object.`;
  if (
    value.status !== undefined &&
    (!Number.isInteger(value.status) ||
      (value.status as number) < 100 ||
      (value.status as number) > 599)
  ) {
    return `${prefix} rejection status must be an HTTP status from 100 to 599.`;
  }
  if (value.body !== undefined && typeof value.body !== "string") {
    return `${prefix} rejection body must be a string.`;
  }
  return validateOptionalObject(value.headers, `${prefix} rejection headers`);
}

function validateMcpGuardrails(value: JsonRecord): string | null {
  if (!Array.isArray(value.processors) || value.processors.length === 0) {
    return "MCP guardrails need at least one processor.";
  }
  for (const [index, candidate] of value.processors.entries()) {
    const prefix = `MCP processor ${index + 1}`;
    if (!isRecord(candidate)) return `${prefix} must be a JSON object.`;
    if (candidate.kind !== "remote") return `${prefix} kind must be remote.`;
    const targetError = validateTarget(candidate, prefix);
    if (targetError) return targetError;
    if (
      candidate.failureMode !== undefined &&
      candidate.failureMode !== "failClosed" &&
      candidate.failureMode !== "failOpen"
    ) {
      return `${prefix} failure mode must be failClosed or failOpen.`;
    }
    if (!isRecord(candidate.methods) || Object.keys(candidate.methods).length === 0) {
      return `${prefix} needs at least one method match.`;
    }
    for (const [pattern, phase] of Object.entries(candidate.methods)) {
      if (!validMethodPattern(pattern)) return `${prefix} has an invalid method pattern.`;
      if (!mcpPhases.includes(phase as (typeof mcpPhases)[number])) {
        return `${prefix} has an invalid method phase.`;
      }
    }
    if (candidate.metadata !== undefined) {
      if (!isRecord(candidate.metadata)) return `${prefix} metadata must be a JSON object.`;
      if (Object.values(candidate.metadata).some((item) => typeof item !== "string")) {
        return `${prefix} metadata values must be CEL strings.`;
      }
    }
    if (candidate.requestHeaders !== undefined) {
      if (!isRecord(candidate.requestHeaders)) {
        return `${prefix} request headers must be a JSON object.`;
      }
      for (const field of ["allowed", "disallowed"] as const) {
        const entries = candidate.requestHeaders[field];
        if (
          entries !== undefined &&
          (!Array.isArray(entries) || entries.some((item) => typeof item !== "string"))
        ) {
          return `${prefix} ${field} request headers must be a string array.`;
        }
      }
    }
    const policiesError = validateOptionalObject(candidate.policies, `${prefix} backend policies`);
    if (policiesError) return policiesError;
  }
  return null;
}

function validateTarget(value: unknown, prefix: string): string | null {
  const target = objectValue(value);
  const variants = [
    typeof target.host === "string" && target.host.trim() ? "host" : null,
    typeof target.backend === "string" && target.backend.trim() ? "backend" : null,
    isRecord(target.service) ? "service" : null,
  ].filter(Boolean);
  if (variants.length !== 1) return `${prefix} needs exactly one host, backend, or service target.`;
  if (typeof target.host === "string" && target.host.trim()) {
    const host = target.host.trim();
    if (host.startsWith("unix:")) {
      if (!host.slice("unix:".length).trim()) return `${prefix} Unix socket path is required.`;
    } else {
      const separator = host.indexOf(":");
      const hostname = separator >= 0 ? host.slice(0, separator) : "";
      const port = separator >= 0 ? host.slice(separator + 1) : "";
      if (!hostname || !/^\d+$/.test(port) || Number(port) < 0 || Number(port) > 65535) {
        return `${prefix} host must use connection host:port or unix:/path format.`;
      }
    }
  }
  if (isRecord(target.service)) {
    if (typeof target.service.name !== "string" || !target.service.name.trim()) {
      return `${prefix} service name is required.`;
    }
    const [namespace, hostname, ...rest] = target.service.name.split("/");
    if (!namespace || !hostname || rest.length) {
      return `${prefix} service name must use namespace/hostname format.`;
    }
    if (
      !Number.isInteger(target.service.port) ||
      (target.service.port as number) < 0 ||
      (target.service.port as number) > 65535
    ) {
      return `${prefix} service port must be from 0 to 65535.`;
    }
  }
  return null;
}

function validateOptionalObject(value: unknown, label: string): string | null {
  if (value === undefined || value === null || isRecord(value)) return null;
  return `${label} must be a JSON object or null.`;
}

function validMethodPattern(pattern: string) {
  if (!pattern) return false;
  const stars = [...pattern].filter((character) => character === "*").length;
  if (stars === 0) return true;
  return stars === 1 && (pattern.startsWith("*") || pattern.endsWith("*"));
}
