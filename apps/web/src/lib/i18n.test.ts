import { describe, expect, it } from "vitest";

import { getBeijingGreeting, translate } from "./i18n";

describe("console internationalization", () => {
  it("selects the greeting from the current Beijing hour", () => {
    expect(getBeijingGreeting(new Date("2026-07-20T18:00:00Z"), "en")).toContain("It's late");
    expect(getBeijingGreeting(new Date("2026-07-21T00:00:00Z"), "en")).toContain("Good morning");
    expect(getBeijingGreeting(new Date("2026-07-21T06:00:00Z"), "en")).toContain("Good afternoon");
    expect(getBeijingGreeting(new Date("2026-07-21T12:00:00Z"), "en")).toContain("Good evening");
  });

  it("translates Chinese messages and interpolates variables", () => {
    expect(translate("Home", "zh-CN")).toBe("首页");
    expect(translate("{count} pending approvals", "zh-CN", { count: 2 })).toBe("2 项待审批");
    expect(getBeijingGreeting(new Date("2026-07-21T00:00:00Z"), "zh-CN")).toContain("早上好");
  });
});
