import { createContext, useContext, useEffect, useRef, useState } from "react";

import type { UnifiedEvent } from "../types";
import type { TraceSummary } from "../generated/api-client";
import { getScenario, withScenario } from "./api";

export function useLiveEvents(enabled = true) {
  const [events, setEvents] = useState<UnifiedEvent[]>([]);
  const [status, setStatus] = useState<"connecting" | "live" | "paused">("connecting");
  const [revision, setRevision] = useState(0);
  const [traceUpdates, setTraceUpdates] = useState<TraceSummary[]>([]);
  const [traceRevision, setTraceRevision] = useState(0);
  const [resetRevision, setResetRevision] = useState(0);
  const seenDeliveries = useRef(new Set<string>());
  const deliveryOrder = useRef<string[]>([]);

  useEffect(() => {
    if (!enabled || getScenario() === "loading" || getScenario() === "error") {
      setStatus("paused");
      return;
    }

    const source = new EventSource(withScenario("/api/v1/stream"));
    const handleEvent = (message: MessageEvent<string>) => {
      let event: UnifiedEvent;
      try {
        event = JSON.parse(message.data) as UnifiedEvent;
      } catch {
        return;
      }
      if (!rememberDelivery(message.lastEventId, seenDeliveries.current, deliveryOrder.current))
        return;
      setEvents((current) => mergeLiveEvents([event], current));
      setRevision((current) => current + 1);
      setStatus(document.hidden ? "paused" : "live");
    };
    const handleReset = (message: MessageEvent<string>) => {
      if (!rememberDelivery(message.lastEventId, seenDeliveries.current, deliveryOrder.current))
        return;
      seenDeliveries.current.clear();
      deliveryOrder.current = [];
      rememberDelivery(message.lastEventId, seenDeliveries.current, deliveryOrder.current);
      setEvents([]);
      setTraceUpdates([]);
      setResetRevision((current) => current + 1);
      setStatus(document.hidden ? "paused" : "live");
    };
    const handleTrace = (message: MessageEvent<string>) => {
      let summary: TraceSummary;
      try {
        summary = JSON.parse(message.data) as TraceSummary;
      } catch {
        return;
      }
      if (!isTraceSummary(summary)) return;
      if (!rememberDelivery(message.lastEventId, seenDeliveries.current, deliveryOrder.current))
        return;
      setTraceUpdates((current) => mergeTraceUpdates([summary], current));
      setTraceRevision((current) => current + 1);
      setStatus(document.hidden ? "paused" : "live");
    };
    for (const name of ["traffic", "decision", "approval", "audit", "health"]) {
      source.addEventListener(name, handleEvent as EventListener);
    }
    source.addEventListener("trace", handleTrace as EventListener);
    source.addEventListener("reset", handleReset as EventListener);
    source.onopen = () => setStatus(document.hidden ? "paused" : "live");
    source.onerror = () => setStatus(document.hidden ? "paused" : "connecting");
    const visibility = () => {
      setStatus(
        document.hidden ? "paused" : source.readyState === EventSource.OPEN ? "live" : "connecting",
      );
    };
    document.addEventListener("visibilitychange", visibility);
    return () => {
      document.removeEventListener("visibilitychange", visibility);
      source.close();
    };
  }, [enabled]);

  return { events, traceUpdates, status, revision, traceRevision, resetRevision };
}

export type LiveEventsState = ReturnType<typeof useLiveEvents>;

export const LiveEventsContext = createContext<LiveEventsState | undefined>(undefined);

export function useSharedLiveEvents(): LiveEventsState {
  const value = useContext(LiveEventsContext);
  if (!value) throw new Error("useSharedLiveEvents must be used inside the application shell");
  return value;
}

export function mergeLiveEvents(
  preferred: UnifiedEvent[],
  existing: UnifiedEvent[],
  capacity = 1000,
): UnifiedEvent[] {
  const merged: UnifiedEvent[] = [];
  const ids = new Set<string>();
  for (const event of [...preferred, ...existing]) {
    const id = `${event.source}\u0000${event.id}`;
    if (ids.has(id)) continue;
    ids.add(id);
    merged.push(event);
    if (merged.length === capacity) break;
  }
  return merged;
}

export function mergeTraceUpdates(
  preferred: TraceSummary[],
  existing: TraceSummary[],
  capacity = 1000,
): TraceSummary[] {
  const merged: TraceSummary[] = [];
  const traceIDs = new Set<string>();
  for (const summary of [...preferred, ...existing]) {
    if (traceIDs.has(summary.traceId)) continue;
    traceIDs.add(summary.traceId);
    merged.push(summary);
    if (merged.length === capacity) break;
  }
  return merged;
}

function isTraceSummary(value: TraceSummary): boolean {
  return (
    Boolean(value) &&
    typeof value === "object" &&
    typeof value.traceId === "string" &&
    /^[0-9a-f]{32}$/.test(value.traceId) &&
    typeof value.updatedAt === "string" &&
    ["running", "succeeded", "failed", "unknown"].includes(value.status)
  );
}

function rememberDelivery(id: string, seen: Set<string>, order: string[]): boolean {
  if (!id) return true;
  if (seen.has(id)) return false;
  seen.add(id);
  order.push(id);
  if (order.length > 1000) {
    const expired = order.shift();
    if (expired) seen.delete(expired);
  }
  return true;
}
