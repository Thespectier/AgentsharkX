import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { DemoRunEvent } from "../generated/api-client";
import { useDemoRunEvents } from "./use-demo-run-events";

const runId = "11111111-1111-4111-8111-111111111111";

function event(overrides: Partial<DemoRunEvent> = {}): DemoRunEvent {
  return {
    runId,
    type: "run.step",
    status: "running",
    runVersion: 2,
    stepId: "plan",
    completedSteps: 1,
    totalSteps: 8,
    occurredAt: "2026-07-30T08:00:01Z",
    ...overrides,
  };
}

class FakeEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static current: FakeEventSource | undefined;

  readonly url: string;
  readyState = FakeEventSource.CONNECTING;
  closed = false;
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
    this.closed = true;
    this.readyState = FakeEventSource.CLOSED;
  }

  open() {
    this.readyState = FakeEventSource.OPEN;
    this.onopen?.(new Event("open"));
  }

  fail() {
    this.readyState = FakeEventSource.CONNECTING;
    this.onerror?.(new Event("error"));
  }

  emit(type: string, data: unknown, lastEventId: string) {
    const message = new MessageEvent(type, { data: JSON.stringify(data), lastEventId });
    for (const listener of this.listeners.get(type) ?? []) listener(message);
  }
}

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { client, invalidate, wrapper };
}

beforeEach(() => {
  FakeEventSource.current = undefined;
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("useDemoRunEvents", () => {
  it("deduplicates deliveries and reloads the snapshot after one reset", () => {
    const { wrapper } = setup();
    const { result } = renderHook(
      () => useDemoRunEvents({ runId, enabled: true, terminal: false }),
      { wrapper },
    );
    const source = FakeEventSource.current!;

    act(() => source.emit("run.step", event(), "10"));
    expect(result.current.events).toEqual([event()]);
    expect(result.current.revision).toBe(1);
    expect(result.current.lastEventId).toBe("10");

    act(() => source.emit("run.step", event({ runVersion: 3 }), "10"));
    expect(result.current.revision).toBe(1);

    act(() => source.emit("reset", { reason: "outbox_retention", resumeAfter: 20 }, "20"));
    expect(result.current.events).toEqual([]);
    expect(result.current.resetRevision).toBe(1);

    act(() => source.emit("reset", { reason: "outbox_retention", resumeAfter: 20 }, "20"));
    expect(result.current.resetRevision).toBe(1);

    act(() => source.emit("run.step", event({ runVersion: 4, stepId: "asset_lookup" }), "21"));
    expect(result.current.events).toHaveLength(1);
    expect(result.current.lastEventId).toBe("21");
  });

  it("polls every two seconds only while the stream is disconnected", () => {
    vi.useFakeTimers();
    const { invalidate, wrapper } = setup();
    const { result } = renderHook(
      () => useDemoRunEvents({ runId, enabled: true, terminal: false }),
      { wrapper },
    );
    const source = FakeEventSource.current!;

    act(() => source.fail());
    expect(result.current.status).toBe("polling");
    const immediateCalls = invalidate.mock.calls.length;

    act(() => vi.advanceTimersByTime(1_999));
    expect(invalidate).toHaveBeenCalledTimes(immediateCalls);
    act(() => vi.advanceTimersByTime(1));
    expect(invalidate.mock.calls.length).toBeGreaterThan(immediateCalls);

    act(() => source.open());
    const resumedCalls = invalidate.mock.calls.length;
    act(() => vi.advanceTimersByTime(4_000));
    expect(result.current.status).toBe("live");
    expect(invalidate).toHaveBeenCalledTimes(resumedCalls);
  });

  it("closes the stream as soon as a terminal event arrives", () => {
    const { wrapper } = setup();
    const { result } = renderHook(
      () => useDemoRunEvents({ runId, enabled: true, terminal: false }),
      { wrapper },
    );
    const source = FakeEventSource.current!;

    act(() =>
      source.emit(
        "run.finished",
        event({ type: "run.finished", status: "succeeded", outcome: "normal", runVersion: 9 }),
        "30",
      ),
    );

    expect(source.closed).toBe(true);
    expect(result.current.status).toBe("closed");
  });
});
