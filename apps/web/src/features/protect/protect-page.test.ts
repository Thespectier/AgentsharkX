import { describe, expect, it } from "vitest";

import { protectApprovals } from "../../mocks/data";
import type { UnifiedEvent } from "../../generated/api-client";
import { approvalForTicketId, protectionStats } from "./protect-page";

describe("Protect approval deep links", () => {
  it("locates a ticket by its opaque public ID, never its upstream ID", () => {
    const approval = protectApprovals[0];

    expect(approvalForTicketId(protectApprovals, approval.id)).toBe(approval);
    expect(approvalForTicketId(protectApprovals, approval.upstreamId)).toBeUndefined();
  });
});

describe("Protect overview outcomes", () => {
  it("counts only explicit runtime and completed approval decisions", () => {
    const event = (overrides: Partial<UnifiedEvent>): UnifiedEvent => ({
      id: String(Math.random()),
      timestamp: "2026-07-30T08:00:00Z",
      source: "agentguard",
      kind: "decision",
      severity: "info",
      summary: "explicit decision",
      rawRef: { source: "agentguard", id: "decision" },
      ...overrides,
    });

    expect(
      protectionStats([
        event({ decision: "ALLOW" }),
        event({ decision: "DENY" }),
        event({ kind: "approval", decision: "APPROVE" }),
        event({ kind: "approval", decision: "DENY" }),
        event({ decision: "HUMAN_CHECK" }),
        event({
          source: "agentgateway",
          decision: "DENY",
          rawRef: { source: "agentgateway", id: "request" },
        }),
      ]),
    ).toEqual({ passed: 1, blocked: 1, approved: 1, denied: 1 });
  });
});
