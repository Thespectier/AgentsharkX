import { expect, test } from "@playwright/test";

const workspaces = [
  ["/", "Good afternoon. Your agents are in control."],
  ["/connect/overview", "Connect agents to every destination"],
  ["/trust/agents", "Know what every agent can reach"],
  ["/protect/policies", "Enforce every critical boundary"],
  ["/audit/analytics", "See every verified signal"],
] as const;

test("all five primary pages render from labelled mock data", async ({ page }) => {
  for (const [path, heading] of workspaces) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
    await expect(page.getByText("MOCK DATA")).toBeVisible();
  }
});

test("empty, loading, partial, and total failure states are explicit", async ({ page }) => {
  await page.goto("/?scenario=empty");
  await expect(
    page.getByRole("heading", { name: "Bring your control plane online" }),
  ).toBeVisible();

  await page.goto("/?scenario=loading");
  await expect(page.getByRole("status", { name: "Loading runtime posture" })).toHaveAttribute(
    "aria-busy",
    "true",
  );

  await page.goto("/?scenario=partial");
  await expect(page.getByRole("status").filter({ hasText: "Partial data" })).toContainText(
    "AgentGuard",
  );
  await expect(page.getByRole("heading", { name: /agents are in control/i })).toBeVisible();

  await page.goto("/?scenario=error");
  await expect(page.getByRole("heading", { name: "Control plane unavailable" })).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("All sources are unavailable");
});

test("an audit detail drawer is recoverable from its URL", async ({ page }) => {
  await page.goto("/audit/security-events");
  const row = page.locator("tbody tr").first();
  await row.click();
  await expect(page).toHaveURL(/\/audit\/security-events\?event=/);
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  const title = await dialog.getByRole("heading", { level: 2 }).textContent();

  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(row).toBeFocused();
  await row.click();

  await page.reload();
  await expect(page.getByRole("dialog").getByRole("heading", { level: 2 })).toHaveText(title ?? "");
});

test("the command palette supports keyboard navigation", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /agents are in control/i })).toBeVisible();
  await page.keyboard.press("Control+k");
  const input = page.getByRole("combobox", { name: "Search commands" });
  await expect(input).toBeFocused();
  await input.fill("Open Trust");
  await input.press("Enter");
  await expect(page).toHaveURL(/\/trust\/agents$/);
  await expect(
    page.getByRole("heading", { name: "Know what every agent can reach" }),
  ).toBeVisible();
});

test("reduced motion removes continuous animation", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await expect(page.locator(".live-flow")).toHaveAttribute("data-motion", "reduced");
  await expect(page.locator("animateMotion")).toHaveCount(0);
  const ambientAnimation = await page.evaluate(
    () => getComputedStyle(document.body, "::before").animationName,
  );
  expect(ambientAnimation).toBe("none");
});
