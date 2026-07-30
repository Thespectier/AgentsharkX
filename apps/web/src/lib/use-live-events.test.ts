import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { UnifiedEvent } from "../types";
import type { TraceSummary } from "../generated/api-client";
import { mergeLiveEvents, mergeTraceUpdates, useLiveEvents } from "./use-live-events";

function event(id: number, source: UnifiedEvent["source"] = "agentgateway"): UnifiedEvent {
  return {
    id: `gateway:${id}`,
    timestamp: new Date(1_780_000_000_000 + id).toISOString(),
    source,
    kind: "traffic",
    severity: "info",
    summary: `request ${id}`,
    rawRef: { source, id: String(id) },
  };
}

function trace(traceId = "11111111111111111111111111111111"): TraceSummary {
  return {
    traceId,
    status: "running",
    completeness: "partial",
    startedAt: "2026-07-30T08:00:00Z",
    llmCalls: 1,
    toolCalls: 0,
    mcpCalls: 0,
    localToolCalls: 0,
    a2aCalls: 0,
    retrieverCalls: 0,
    inputTokens: 10,
    outputTokens: 0,
    totalTokens: 10,
    errorCount: 0,
    spanCount: 2,
    lastSpanAt: "2026-07-30T08:00:01Z",
    updatedAt: "2026-07-30T08:00:01Z",
  };
}

class FakeEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static current: FakeEventSource | undefined;

  readonly url: string;
  readyState = FakeEventSource.CONNECTING;
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  private readonly listeners = new Map<string, Set<EventListener>>();

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.current = this;
  }

  addEventListener(type: string, listener: EventListener | null) {
    if (!listener) return;
    const listeners = this.listeners.get(type) ?? new Set<EventListener>();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  close() {
    this.readyState = FakeEventSource.CLOSED;
  }

  emit(type: string, data: unknown, lastEventId: string) {
    const message = new MessageEvent(type, { data: JSON.stringify(data), lastEventId });
    for (const listener of this.listeners.get(type) ?? []) listener(message);
  }
}

beforeEach(() => {
  FakeEventSource.current = undefined;
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("mergeLiveEvents", () => {
  it("deduplicates reconnect replay and bounds a 5000-event burst", () => {
    const burst = Array.from({ length: 5000 }, (_, index) => event(index)).reverse();
    const replay = burst.slice(0, 250);
    const merged = mergeLiveEvents(replay, burst, 1000);

    expect(merged).toHaveLength(1000);
    expect(new Set(merged.map((item) => item.id))).toHaveLength(1000);
    expect(merged[0]?.id).toBe("gateway:4999");
    expect(merged.at(-1)?.id).toBe("gateway:4000");
  });

  it("upserts an updated event by source and ID while preserving another source", () => {
    const original = event(1);
    const updated = { ...original, summary: "updated request" };
    const guardEvent = event(1, "agentguard");

    expect(mergeLiveEvents([updated, guardEvent], [original])).toEqual([updated, guardEvent]);
  });
});

describe("mergeTraceUpdates", () => {
  it("upserts summary-only updates by explicit Trace ID", () => {
    const original = trace();
    const updated = { ...original, spanCount: 3, updatedAt: "2026-07-30T08:00:02Z" };
    expect(mergeTraceUpdates([updated], [original])).toEqual([updated]);
  });
});

describe("useLiveEvents", () => {
  it("deduplicates deliveries by SSE ID and accepts updates to one business event", () => {
    const { result } = renderHook(() => useLiveEvents());
    const source = FakeEventSource.current!;
    const original = event(1);
    const updated = { ...original, summary: "updated request" };

    act(() => source.emit("traffic", original, "10"));
    expect(result.current.revision).toBe(1);
    expect(result.current.events).toEqual([original]);

    act(() => source.emit("traffic", updated, "11"));
    expect(result.current.revision).toBe(2);
    expect(result.current.events).toEqual([updated]);

    act(() => source.emit("traffic", { ...updated, summary: "duplicate delivery" }, "11"));
    expect(result.current.revision).toBe(2);
    expect(result.current.events).toEqual([updated]);

    const guardEvent = event(1, "agentguard");
    act(() => source.emit("audit", guardEvent, "12"));
    expect(result.current.events).toEqual([guardEvent, updated]);
  });

  it("clears buffered events once for a reset delivery and resumes afterward", () => {
    const { result } = renderHook(() => useLiveEvents());
    const source = FakeEventSource.current!;

    act(() => source.emit("traffic", event(1), "10"));
    act(() => source.emit("reset", { reason: "outbox_retention", resumeAfter: 20 }, "20"));

    expect(result.current.events).toEqual([]);
    expect(result.current.revision).toBe(1);
    expect(result.current.resetRevision).toBe(1);

    act(() => source.emit("reset", { reason: "outbox_retention", resumeAfter: 20 }, "20"));
    expect(result.current.resetRevision).toBe(1);

    act(() => source.emit("traffic", event(2), "21"));
    expect(result.current.events).toEqual([event(2)]);
    expect(result.current.revision).toBe(2);
  });

  it("keeps Trace summary deliveries separate from Audit events", () => {
    const { result } = renderHook(() => useLiveEvents());
    const source = FakeEventSource.current!;
    const summary = trace();

    act(() => source.emit("trace", summary, "trace-10"));
    expect(result.current.traceRevision).toBe(1);
    expect(result.current.traceUpdates).toEqual([summary]);
    expect(result.current.events).toEqual([]);

    act(() => source.emit("trace", { ...summary, spanCount: 99 }, "trace-10"));
    expect(result.current.traceRevision).toBe(1);
    expect(result.current.traceUpdates).toEqual([summary]);
  });
});
