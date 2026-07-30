import { describe, expect, it } from "vitest";

import { parseSearch } from "./router";

describe("root search schema", () => {
  it("preserves exact Demo evidence identifiers", () => {
    expect(
      parseSearch({
        scenario: "partial",
        event: "guard:event-1",
        sessionId: "demo-session-1",
        ticketId: "opaque-ticket-1",
      }),
    ).toEqual({
      scenario: "partial",
      event: "guard:event-1",
      sessionId: "demo-session-1",
      ticketId: "opaque-ticket-1",
    });
  });

  it("drops non-string identifiers and unknown scenarios", () => {
    expect(parseSearch({ scenario: "unknown", sessionId: 7, ticketId: false })).toEqual({
      scenario: undefined,
      event: undefined,
      sessionId: undefined,
      ticketId: undefined,
    });
  });
});
