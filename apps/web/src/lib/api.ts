import type { ApiFailure, Envelope, Scenario } from "../types";

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

export function withScenario(path: string): string {
  const scenario = getScenario();
  const url = new URL(path, window.location.origin);
  if (scenario !== "normal") url.searchParams.set("scenario", scenario);
  return `${url.pathname}${url.search}`;
}

export async function requestEnvelope<T>(path: string, signal?: AbortSignal): Promise<Envelope<T>> {
  const response = await fetch(withScenario(path), {
    credentials: "include",
    headers: { Accept: "application/json" },
    signal,
  });
  if (!response.ok) {
    const body = (await response.json().catch(() => undefined)) as ApiFailure | undefined;
    throw new ApiError(
      response.status,
      body?.error.message ?? `${response.status} ${response.statusText}`,
      body?.error,
    );
  }
  return (await response.json()) as Envelope<T>;
}

export function formatError(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return "The request failed for an unknown reason.";
}
