import { describe, expect, it } from "vitest";

import type { UnifiedEvent } from "../types";
import { mergeLiveEvents } from "./use-live-events";

function event(id: number): UnifiedEvent {
  return {
    id: `gateway:${id}`,
    timestamp: new Date(1_780_000_000_000 + id).toISOString(),
    source: "agentgateway",
    kind: "traffic",
    severity: "info",
    summary: `request ${id}`,
    rawRef: { source: "agentgateway", id: String(id) },
  };
}

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
});
