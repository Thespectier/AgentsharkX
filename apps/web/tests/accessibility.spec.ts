import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const pages = [
  "/",
  "/connect/overview",
  "/connect/mcp",
  "/connect/traffic",
  "/trust/agents",
  "/protect/policies",
  "/protect/guardrails",
  "/audit/analytics",
];

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

test("gateway policy editor has no serious or critical accessibility violations", async ({
  page,
}) => {
  await page.goto("/protect/policies");
  await page
    .getByRole("row", { name: /Basic auth/ })
    .getByRole("button", { name: "Configure policy: Basic auth" })
    .click();
  const dialog = page.getByRole("dialog", { name: "Configure Basic auth" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveCSS("opacity", "1");
  const results = await new AxeBuilder({ page }).include("[role=dialog]").analyze();
  const blocking = results.violations.filter(({ impact }) =>
    impact ? ["serious", "critical"].includes(impact) : false,
  );
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([]);
});

test("gateway guardrail editor has no serious or critical accessibility violations", async ({
  page,
}) => {
  await page.goto("/protect/guardrails");
  await page.getByRole("button", { name: "Edit guardrails" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Guardrails" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveCSS("opacity", "1");
  const results = await new AxeBuilder({ page }).include("[role=dialog]").analyze();
  const blocking = results.violations.filter(({ impact }) =>
    impact ? ["serious", "critical"].includes(impact) : false,
  );
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([]);
});
