import { createContext, useContext, useEffect, useRef, useState } from "react";

import type { UnifiedEvent } from "../types";
import { getScenario, withScenario } from "./api";

export function useLiveEvents(enabled = true) {
  const [events, setEvents] = useState<UnifiedEvent[]>([]);
  const [status, setStatus] = useState<"connecting" | "live" | "paused">("connecting");
  const [revision, setRevision] = useState(0);
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
      setResetRevision((current) => current + 1);
      setStatus(document.hidden ? "paused" : "live");
    };
    for (const name of ["traffic", "decision", "approval", "audit", "health"]) {
      source.addEventListener(name, handleEvent as EventListener);
    }
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

  return { events, status, revision, resetRevision };
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
