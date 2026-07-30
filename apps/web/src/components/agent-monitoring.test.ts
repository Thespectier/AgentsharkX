import { describe, expect, it } from "vitest";

import type { TrustAgent } from "../generated/api-client";
import { reportedRunningCount } from "./agent-monitoring";

describe("Agent monitoring metrics", () => {
  it("keeps running count unavailable when the management contract reports unknown state", () => {
    const agent = { status: "unknown" } as TrustAgent;

    expect(reportedRunningCount([agent])).toBeNull();
    expect(reportedRunningCount([])).toBeNull();
  });
});
