import type { ApiFailure, Envelope, Scenario } from "../types";
import {
  implementedOperations,
  type ImplementedOperationId,
  type OperationBodies,
  type OperationResponse,
} from "../generated/api-client";

type ReadOperation = Exclude<ImplementedOperationId, "createAdminSession">;

interface OperationOptions {
  signal?: AbortSignal;
  path?: Record<string, string>;
  query?: Record<string, string | number | undefined>;
}

let csrfToken: string | undefined;

export class ApiError extends Error {
  readonly status: number;
  readonly failure?: ApiFailure["error"];

  constructor(status: number, message: string, failure?: ApiFailure["error"]) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.failure = failure;
  }
}

export function getScenario(): Scenario {
  if (typeof window === "undefined") return "normal";
  const value = new URLSearchParams(window.location.search).get("scenario");
  if (["empty", "loading", "partial", "error"].includes(value ?? "")) {
    return value as Scenario;
  }
  return "normal";
}

export function isMockMode(): boolean {
  return import.meta.env.VITE_ENABLE_MOCKS !== "false";
}

export function withScenario(path: string): string {
  const scenario = getScenario();
  const url = new URL(path, window.location.origin);
  if (isMockMode() && scenario !== "normal") url.searchParams.set("scenario", scenario);
  return `${url.pathname}${url.search}`;
}

export async function requestEnvelope<T>(path: string, signal?: AbortSignal): Promise<Envelope<T>> {
  return requestJSON<Envelope<T>>(path, { signal });
}

export async function requestOperation<K extends ReadOperation>(
  operation: K,
  options?: AbortSignal | OperationOptions,
): Promise<OperationResponse<K>> {
  const normalized = options instanceof AbortSignal ? { signal: options } : (options ?? {});
  let path: string = implementedOperations[operation].path;
  for (const [name, value] of Object.entries(normalized.path ?? {})) {
    path = path.replace(`{${name}}`, encodeURIComponent(value));
  }
  if (path.includes("{")) throw new Error(`Missing path parameter for ${operation}`);
  const url = new URL(path, window.location.origin);
  for (const [name, value] of Object.entries(normalized.query ?? {})) {
    if (value !== undefined && value !== "") url.searchParams.set(name, String(value));
  }
  return requestJSON<OperationResponse<K>>(`${url.pathname}${url.search}`, {
    signal: normalized.signal,
  });
}

export async function createAdminSession(
  body: OperationBodies["createAdminSession"],
): Promise<void> {
  const operation = implementedOperations.createAdminSession;
  const response = await fetch(operation.path, {
    method: operation.method,
    credentials: "include",
    headers: { Accept: "application/json", "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw await apiError(response);
  const issuedCSRF = response.headers.get("X-CSRF-Token");
  if (!issuedCSRF) {
    throw new ApiError(response.status, "The server did not issue write protection.");
  }
  csrfToken = issuedCSRF;
}

export async function requestMutation<T>(
  path: string,
  method: "POST" | "PATCH" | "DELETE",
  body?: unknown,
): Promise<T | undefined> {
  const response = await fetch(path, {
    method,
    credentials: "include",
    headers: {
      Accept: "application/json",
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      ...(csrfToken ? { "X-CSRF-Token": csrfToken } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) throw await apiError(response);
  if (response.status === 204) return undefined;
  return (await response.json()) as T;
}

async function requestJSON<T>(path: string, options: { signal?: AbortSignal } = {}): Promise<T> {
  const response = await fetch(withScenario(path), {
    credentials: "include",
    headers: { Accept: "application/json" },
    signal: options.signal,
  });
  if (!response.ok) {
    const error = await apiError(response);
    if (response.status === 401 && typeof window !== "undefined") {
      window.dispatchEvent(new Event("agentshark:unauthorized"));
    }
    throw error;
  }
  return (await response.json()) as T;
}

async function apiError(response: Response): Promise<ApiError> {
  const body = (await response.json().catch(() => undefined)) as ApiFailure | undefined;
  return new ApiError(
    response.status,
    body?.error.message ?? `${response.status} ${response.statusText}`,
    body?.error,
  );
}

export function formatError(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return "The request failed for an unknown reason.";
}
