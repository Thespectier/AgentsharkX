import { describe, expect, it } from "vitest";

import { formatDuration, formatTrendTick, formatTrendTimestamp } from "./format";

describe("trend formatting", () => {
  it("renders exact UTC bucket timestamps without browser timezone drift", () => {
    const timestamp = "2026-07-21T12:05:00Z";

    expect(formatTrendTick(timestamp)).toBe("12:05");
    expect(formatTrendTimestamp(timestamp)).toContain("12:05");
    expect(formatTrendTimestamp(timestamp)).toContain("Jul");
  });

  it("preserves invalid labels and one decimal place of latency precision", () => {
    expect(formatTrendTick("unknown")).toBe("unknown");
    expect(formatDuration(123.45)).toBe("123.5 ms");
  });
});
