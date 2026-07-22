import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const pages = ["/", "/connect/overview", "/trust/agents", "/protect/policies", "/audit/analytics"];

test.use({ reducedMotion: "reduce" });

for (const path of pages) {
  test(`${path} has no serious or critical accessibility violations`, async ({ page }) => {
    await page.goto(path);
    await expect(page.locator("h1")).toBeVisible();
    const results = await new AxeBuilder({ page }).analyze();
    const blocking = results.violations.filter(({ impact }) =>
      impact ? ["serious", "critical"].includes(impact) : false,
    );
    const summary = blocking.flatMap((violation) =>
      violation.nodes.map((node) => ({
        id: violation.id,
        target: node.target,
        message: node.any[0]?.message ?? node.failureSummary,
      })),
    );
    expect(blocking.length, JSON.stringify(summary, null, 2)).toBe(0);
  });
}
