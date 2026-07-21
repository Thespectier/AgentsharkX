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
