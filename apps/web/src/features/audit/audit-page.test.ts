import { describe, expect, it } from "vitest";

import { baseEvents } from "../../mocks/data";
import { filterAuditEvents } from "./audit-page";

describe("audit filters", () => {
  it("filters by verified source, severity, and displayed event fields", () => {
    expect(
      filterAuditEvents(baseEvents, {
        source: "agentguard",
        severity: "critical",
        query: "shell invocation",
      }).map((event) => event.id),
    ).toEqual(["guard-audit-9013"]);
  });

  it("returns every event when filters are reset", () => {
    expect(
      filterAuditEvents(baseEvents, { source: "all", severity: "all", query: "" }),
    ).toHaveLength(baseEvents.length);
  });
});
