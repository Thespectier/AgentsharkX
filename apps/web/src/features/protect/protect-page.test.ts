import { describe, expect, it } from "vitest";

import { protectApprovals } from "../../mocks/data";
import { approvalForTicketId } from "./protect-page";

describe("Protect approval deep links", () => {
  it("locates a ticket by its opaque public ID, never its upstream ID", () => {
    const approval = protectApprovals[0];

    expect(approvalForTicketId(protectApprovals, approval.id)).toBe(approval);
    expect(approvalForTicketId(protectApprovals, approval.upstreamId)).toBeUndefined();
  });
});
