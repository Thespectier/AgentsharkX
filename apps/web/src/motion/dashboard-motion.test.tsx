import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { UnifiedEvent } from "../types";
import { ActivityRail } from "./dashboard-motion";

const events: UnifiedEvent[] = [
  {
    id: "event-1",
    timestamp: "2026-07-28T01:00:00Z",
    source: "agentguard",
    kind: "audit",
    severity: "high",
    summary: "AgentGuard blocked a protected action",
    rawRef: { source: "agentguard", id: "event-1" },
  },
];

describe("activity rail", () => {
  it("uses Agentshark product language for upstream-owned source names", () => {
    render(<ActivityRail events={events} />);

    expect(screen.getByText("Runtime protection")).toBeVisible();
    expect(screen.getByText("Agentshark Runtime blocked a protected action")).toBeVisible();
    expect(screen.queryByText(/AgentGuard/)).not.toBeInTheDocument();
  });
});
