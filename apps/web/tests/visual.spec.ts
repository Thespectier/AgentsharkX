import { expect, test } from "@playwright/test";

test("desktop home visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /agents are in control/i })).toBeVisible();
  await expect(page).toHaveScreenshot("home-1440.png");
});

test("laptop audit visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/audit/security-events");
  await expect(page.getByRole("heading", { name: "See every verified signal" })).toBeVisible();
  await expect(page).toHaveScreenshot("audit-1280.png");
});

test("laptop Connect visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/connect/overview");
  await expect(
    page.getByRole("heading", { name: "Connect agents to every destination" }),
  ).toBeVisible();
  await expect(page).toHaveScreenshot("connect-1280.png");
});

test("laptop Trust visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/trust/agents");
  await expect(
    page.getByRole("heading", { name: "Know what every agent can reach" }),
  ).toBeVisible();
  await expect(page).toHaveScreenshot("trust-1280.png");
});

test("laptop Protect visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/protect/policies");
  await expect(
    page.getByRole("heading", { name: "Enforce every critical boundary" }),
  ).toBeVisible();
  await expect(page).toHaveScreenshot("protect-1280.png");
});

test("degraded System diagnostic visual baseline", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1200 });
  await page.goto("/system?scenario=partial");
  await expect(
    page.getByRole("heading", { name: "Sources, versions & capabilities" }),
  ).toBeVisible();
  await expect(page.getByText(/AgentGuard management probes are unavailable/)).toBeVisible();
  await expect(page).toHaveScreenshot("system-degraded-1440.png", { fullPage: true });
});
