import { useEffect, useRef, useState } from "react";

import type { UnifiedEvent } from "../types";
import { getScenario, withScenario } from "./api";

export function useLiveEvents(enabled = true) {
  const [events, setEvents] = useState<UnifiedEvent[]>([]);
  const [status, setStatus] = useState<"connecting" | "live" | "paused">("connecting");
  const seen = useRef(new Set<string>());

  useEffect(() => {
    if (!enabled || getScenario() === "loading" || getScenario() === "error") {
      setStatus("paused");
      return;
    }

    const source = new EventSource(withScenario("/api/v1/stream"));
    const handleEvent = (message: MessageEvent<string>) => {
      const event = JSON.parse(message.data) as UnifiedEvent;
      if (seen.current.has(event.id)) return;
      seen.current.add(event.id);
      setEvents((current) => [event, ...current].slice(0, 24));
      setStatus("live");
    };
    for (const name of ["traffic", "decision", "approval", "audit", "health"]) {
      source.addEventListener(name, handleEvent as EventListener);
    }
    source.onopen = () => setStatus("live");
    source.onerror = () => setStatus("connecting");
    return () => source.close();
  }, [enabled]);

  return { events, status };
}
