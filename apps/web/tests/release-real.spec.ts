import { expect, test } from "@playwright/test";

const enabled = process.env.AGENTSHARK_RELEASE_E2E === "1";
const fixtureURL = process.env.AGENTSHARK_RELEASE_FIXTURE_URL ?? "http://127.0.0.1:19001";

test("release path starts, authenticates, connects, emits an event, and approves it", async ({
  page,
  request,
}) => {
  test.skip(!enabled, "Run through scripts/release-e2e.sh.");

  await page.goto("/connect/overview");
  await expect(page.getByRole("heading", { name: "Unlock the control plane" })).toBeVisible();
  await page.getByLabel("Administrator token").fill("release-admin-token-with-entropy");
  await page.getByRole("button", { name: "Continue securely" }).click();

  await expect(
    page.getByRole("heading", { name: "Connect agents to every destination" }),
  ).toBeVisible();
  await page.goto("/connect/setup");
  await expect(page.getByText("Connection verified")).toBeVisible();

  const emitted = await request.post(`${fixtureURL}/__test/emit`);
  expect(emitted.ok()).toBe(true);

  await page.goto("/audit/traffic");
  await expect(page.getByText(/fixture-provider request to fixture-model/)).toBeVisible({
    timeout: 10_000,
  });
  await page.goto("/audit/security-events");
  await expect(page.getByText("tool invoke for mail.send was human_check")).toBeVisible();

  await page.goto("/protect/approvals");
  const ticket = page.getByRole("button", { name: /mail\.send/ });
  await expect(ticket).toBeVisible({ timeout: 10_000 });
  await ticket.click();
  const dialog = page.getByRole("dialog", { name: "Review mail.send" });
  await dialog.getByLabel("Operator note").fill("Release E2E operator review.");
  await dialog
    .getByLabel("I confirm this operator decision for the selected pending ticket.")
    .check();
  await dialog.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Approval ticket approved" }),
  ).toContainText("Request ID");
});
