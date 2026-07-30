import { expect, test } from "@playwright/test";

const realURL = process.env.AGENTSHARK_REAL_CONNECT_URL;
const realAdminToken = process.env.AGENTSHARK_REAL_ADMIN_TOKEN ?? "phase3-admin-token-with-entropy";

test("real mode authenticates and renders the pinned connection empty state", async ({ page }) => {
  test.skip(!realURL, "Set AGENTSHARK_REAL_CONNECT_URL to run against a live Phase 3 BFF.");

  await page.goto(`${realURL}/connect/overview`);
  await expect(page.getByRole("heading", { name: "Unlock the control plane" })).toBeVisible();
  await page.getByLabel("Administrator token").fill(realAdminToken);
  await page.getByRole("button", { name: "Continue securely" }).click();

  await expect(page.getByRole("heading", { name: "Connection overview" })).toBeVisible();
  await expect(page.getByText("Request-log analytics storage is not configured")).toBeVisible();
  await expect(page.getByText("0", { exact: true }).first()).toBeVisible();

  await expect(page.getByRole("link", { name: "Raw Config" })).toHaveCount(0);
});
