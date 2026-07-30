import { expect, test, type Page } from "@playwright/test";

async function waitForStableShell(page: Page) {
  await expect(page.getByRole("link", { name: "3 pending approvals" })).toBeVisible();
}

test("desktop home visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.clock.setFixedTime(new Date("2026-07-21T06:00:00Z"));
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /agents are in control/i })).toBeVisible();
  await waitForStableShell(page);
  await expect(page.getByText("Monitored agents", { exact: true })).toBeVisible();
  await expect(page.getByText("Runtime state is not reported")).toBeVisible();
  await expect(page).toHaveScreenshot("home-1440.png");
});

test("laptop audit visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/audit/traces");
  await expect(
    page.getByRole("heading", { level: 1, name: "Agent Trace", exact: true }),
  ).toBeVisible();
  await waitForStableShell(page);
  await expect(page).toHaveScreenshot("audit-1280.png");
});

test("laptop Connect visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/connect/overview");
  await expect(page.getByRole("heading", { name: "Connection overview" })).toBeVisible();
  await waitForStableShell(page);
  await expect(page).toHaveScreenshot("connect-1280.png");
});

test("laptop Trust visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/trust/overview");
  await expect(page.getByRole("heading", { name: "Trust overview" })).toBeVisible();
  await waitForStableShell(page);
  await expect(page).toHaveScreenshot("trust-1280.png");
});

test("laptop Protect visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/protect/overview");
  await expect(page.getByRole("heading", { name: "Protection overview" })).toBeVisible();
  await waitForStableShell(page);
  await expect(page).toHaveScreenshot("protect-1280.png");
});

test("laptop Guardrails visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/trust/guardrails");
  await expect(page.getByRole("heading", { name: "LLM guardrails" })).toBeVisible();
  await waitForStableShell(page);
  await expect(page).toHaveScreenshot("guardrails-1280.png");
});

test("runtime rule composer visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 800, height: 700 });
  await page.goto("/trust/runtime-rules");
  await waitForStableShell(page);
  await page.getByRole("button", { name: "New rule" }).click();
  await expect(page.getByRole("dialog", { name: "Publish runtime rule" })).toBeVisible();
  await expect(page).toHaveScreenshot("runtime-rule-dialog-800.png");
});

test("mobile monitored Agents visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.clock.setFixedTime(new Date("2026-07-21T06:00:00Z"));
  await page.goto("/connect/agents");
  await expect(page.getByRole("heading", { name: "Monitored Agents" })).toBeVisible();
  await expect(page).toHaveScreenshot("agents-mobile-390.png", { fullPage: true });
});
