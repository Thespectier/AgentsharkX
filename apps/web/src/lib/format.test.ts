import { describe, expect, it } from "vitest";

import {
  formatDateTimeWithZone,
  formatDuration,
  formatTimeWithZone,
  formatTrendTick,
  formatTrendTimestamp,
} from "./format";

describe("trend formatting", () => {
  it("renders exact Beijing-time bucket timestamps without browser timezone drift", () => {
    const timestamp = "2026-07-21T12:05:00Z";

    expect(formatTrendTick(timestamp)).toBe("20:05");
    expect(formatTrendTimestamp(timestamp)).toContain("20:05");
    expect(formatTrendTimestamp(timestamp)).toContain("Jul");
    expect(formatTimeWithZone(timestamp)).toBe("20:05:00 UTC+8");
  });

  it("includes the Beijing calendar date and handles a UTC date boundary", () => {
    expect(formatDateTimeWithZone("2026-07-21T18:05:06Z")).toBe("2026-07-22 02:05:06 UTC+8");
    expect(formatDateTimeWithZone("unknown")).toBe("unknown");
  });

  it("preserves invalid labels and one decimal place of latency precision", () => {
    expect(formatTrendTick("unknown")).toBe("unknown");
    expect(formatDuration(123.45)).toBe("123.5 ms");
  });
});
