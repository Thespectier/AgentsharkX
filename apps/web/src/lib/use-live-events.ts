import { useEffect, useRef, useState } from "react";

import type { UnifiedEvent } from "../types";
import { getScenario, withScenario } from "./api";

export function useLiveEvents(enabled = true) {
  const [events, setEvents] = useState<UnifiedEvent[]>([]);
  const [status, setStatus] = useState<"connecting" | "live" | "paused">("connecting");
  const seen = useRef(new Set<string>());
  const seenOrder = useRef<string[]>([]);

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
      if (seen.current.has(event.id)) return;
      seen.current.add(event.id);
      seenOrder.current.push(event.id);
      if (seenOrder.current.length > 1000) {
        const expired = seenOrder.current.shift();
        if (expired) seen.current.delete(expired);
      }
      setEvents((current) => mergeLiveEvents([event], current));
      setStatus(document.hidden ? "paused" : "live");
    };
    for (const name of ["traffic", "decision", "approval", "audit", "health"]) {
      source.addEventListener(name, handleEvent as EventListener);
    }
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

  return { events, status };
}

export function mergeLiveEvents(
  preferred: UnifiedEvent[],
  existing: UnifiedEvent[],
  capacity = 1000,
): UnifiedEvent[] {
  const merged: UnifiedEvent[] = [];
  const ids = new Set<string>();
  for (const event of [...preferred, ...existing]) {
    if (ids.has(event.id)) continue;
    ids.add(event.id);
    merged.push(event);
    if (merged.length === capacity) break;
  }
  return merged;
}
