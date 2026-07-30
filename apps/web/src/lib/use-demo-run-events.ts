import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import type { DemoRunEnvelope, DemoRunEvent, DemoRunStatus } from "../generated/api-client";
import { withScenario } from "./api";

export type DemoStreamStatus = "idle" | "connecting" | "live" | "polling" | "closed";

const terminalStatuses = new Set<DemoRunStatus>([
  "succeeded",
  "failed",
  "cancelled",
  "interrupted",
  "expired",
]);

export function isDemoRunTerminal(status?: DemoRunStatus): boolean {
  return status ? terminalStatuses.has(status) : false;
}

export function useDemoRunEvents({
  runId,
  enabled = true,
  terminal = false,
}: {
  runId?: string;
  enabled?: boolean;
  terminal?: boolean;
}) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<DemoStreamStatus>("idle");
  const [events, setEvents] = useState<DemoRunEvent[]>([]);
  const [revision, setRevision] = useState(0);
  const [resetRevision, setResetRevision] = useState(0);
  const [lastEventId, setLastEventId] = useState("");
  const seenDeliveries = useRef(new Set<string>());
  const deliveryOrder = useRef<string[]>([]);

  useEffect(() => {
    seenDeliveries.current.clear();
    deliveryOrder.current = [];
    setEvents([]);
    setRevision(0);
    setResetRevision(0);
    setLastEventId("");
  }, [runId]);

  useEffect(() => {
    if (!runId || !enabled || terminal) {
      setStatus(terminal && runId ? "closed" : "idle");
      return;
    }

    let active = true;
    let pollingTimer: number | undefined;
    const source = new EventSource(
      withScenario(`/api/v1/demo/runs/${encodeURIComponent(runId)}/events`),
    );

    const invalidate = (event?: DemoRunEvent) => {
      const cached = queryClient
        .getQueriesData<DemoRunEnvelope>({ queryKey: ["demo-run", runId] })
        .map(([, envelope]) => envelope?.data)
        .find(Boolean);
      const traceId = event?.traceId || cached?.traceId;
      const hasApproval = Boolean(event?.approval || cached?.approval);
      void queryClient.invalidateQueries({ queryKey: ["demo-run", runId] });
      void queryClient.invalidateQueries({ queryKey: ["demo-status"] });
      void queryClient.invalidateQueries({ queryKey: ["demo-runs"] });
      if (traceId) void queryClient.invalidateQueries({ queryKey: ["audit-trace", traceId] });
      if (hasApproval) void queryClient.invalidateQueries({ queryKey: ["protect-approvals"] });
    };
    const stopPolling = () => {
      if (pollingTimer !== undefined) window.clearInterval(pollingTimer);
      pollingTimer = undefined;
    };
    const startPolling = () => {
      if (!active || pollingTimer !== undefined) return;
      setStatus("polling");
      invalidate();
      pollingTimer = window.setInterval(() => invalidate(), 2_000);
    };
    const acceptDelivery = (message: MessageEvent<string>) => {
      if (
        !rememberDemoDelivery(message.lastEventId, seenDeliveries.current, deliveryOrder.current)
      ) {
        return false;
      }
      if (message.lastEventId) setLastEventId(message.lastEventId);
      return true;
    };
    const handleSnapshot = (message: MessageEvent<string>) => {
      if (!acceptDelivery(message)) return;
      stopPolling();
      invalidate();
      setStatus("live");
    };
    const handleEvent = (message: MessageEvent<string>) => {
      let event: DemoRunEvent;
      try {
        event = JSON.parse(message.data) as DemoRunEvent;
      } catch {
        return;
      }
      if (!isDemoRunEvent(event, runId) || !acceptDelivery(message)) return;
      stopPolling();
      setEvents((current) => mergeDemoRunEvents(event, current));
      setRevision((current) => current + 1);
      invalidate(event);
      if (event.type === "run.finished" || isDemoRunTerminal(event.status)) {
        stopPolling();
        source.close();
        setStatus("closed");
      } else {
        setStatus("live");
      }
    };
    const handleReset = (message: MessageEvent<string>) => {
      if (!acceptDelivery(message)) return;
      stopPolling();
      seenDeliveries.current.clear();
      deliveryOrder.current = [];
      rememberDemoDelivery(message.lastEventId, seenDeliveries.current, deliveryOrder.current);
      setEvents([]);
      setResetRevision((current) => current + 1);
      invalidate();
      setStatus("live");
    };

    source.addEventListener("snapshot", handleSnapshot as EventListener);
    for (const name of [
      "run.status",
      "run.step",
      "run.trace_linked",
      "run.approval_linked",
      "run.finished",
    ]) {
      source.addEventListener(name, handleEvent as EventListener);
    }
    source.addEventListener("reset", handleReset as EventListener);
    source.onopen = () => {
      stopPolling();
      setStatus("live");
    };
    source.onerror = startPolling;
    setStatus("connecting");

    return () => {
      active = false;
      stopPolling();
      source.close();
    };
  }, [enabled, queryClient, runId, terminal]);

  return { events, lastEventId, resetRevision, revision, status };
}

export function mergeDemoRunEvents(event: DemoRunEvent, existing: DemoRunEvent[]): DemoRunEvent[] {
  const sameVersion = existing.some(
    (item) => item.runVersion === event.runVersion && item.type === event.type,
  );
  return sameVersion ? existing : [...existing, event].slice(-100);
}

function isDemoRunEvent(value: DemoRunEvent, runId: string): boolean {
  return (
    Boolean(value) &&
    typeof value === "object" &&
    value.runId === runId &&
    typeof value.runVersion === "number" &&
    typeof value.occurredAt === "string" &&
    ["run.status", "run.step", "run.trace_linked", "run.approval_linked", "run.finished"].includes(
      value.type,
    )
  );
}

function rememberDemoDelivery(id: string, seen: Set<string>, order: string[]): boolean {
  if (!id) return true;
  if (seen.has(id)) return false;
  seen.add(id);
  order.push(id);
  if (order.length > 500) {
    const expired = order.shift();
    if (expired) seen.delete(expired);
  }
  return true;
}
