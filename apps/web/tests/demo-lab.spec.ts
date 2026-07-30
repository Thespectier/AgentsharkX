import { expect, test, type Locator } from "@playwright/test";

async function verticalTop(locator: Locator): Promise<number> {
  const box = await locator.boundingBox();
  expect(box).not.toBeNull();
  return box!.y;
}

test("Demo Lab runs the default approval scenario through the shared Protect decision", async ({
  page,
}) => {
  await page.goto("/demo");

  await expect(page.getByRole("heading", { level: 1, name: "Demo Lab" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Demo Lab", exact: true })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.getByText("Demo Runner", { exact: true })).toBeVisible();
  await expect(page.getByRole("radio", { name: /^Approval/ })).toBeChecked();
  await expect(page.getByLabel("Step delay in milliseconds")).toHaveValue("700");

  await page.getByRole("radio", { name: /^Happy path/ }).check();
  await expect(page.getByRole("radio", { name: /^Happy path/ })).toBeChecked();
  await page.getByRole("radio", { name: /^Approval/ }).check();

  const createRequest = page.waitForRequest(
    (request) =>
      request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/demo/runs",
  );
  await page.getByRole("button", { name: "Start Run" }).click();
  const request = await createRequest;
  expect(request.postDataJSON()).toEqual({ scenario: "approval", delayMs: 700 });
  expect(request.headers()["x-request-id"]).toMatch(/^demo-/);

  await expect(page.getByText("waiting approval", { exact: true })).toBeVisible();
  await expect(page.getByText("Linked by exact session_id", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Review decision" }).click();

  const dialog = page.getByRole("dialog", { name: "Review send_http" });
  await dialog.getByLabel("Operator note").fill("Approved during deterministic Demo Lab E2E");
  await dialog
    .getByLabel("I confirm this operator decision for the selected pending ticket.")
    .check();
  await dialog.getByRole("button", { name: "Approve" }).click();

  await expect(dialog).toBeHidden();
  await expect(page.getByText("Approval ticket approved", { exact: true })).toBeVisible();
  await expect(
    page.locator(".demo-run-statuses").getByText("approved", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Linked by exact session_id", { exact: true })).toBeVisible();
});

test("Demo Lab remains ordered, contained, and motion-reduced on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/demo");
  await expect(page.getByRole("heading", { level: 1, name: "Demo Lab" })).toBeVisible();

  const sections = [
    page.locator(".demo-readiness"),
    page.locator(".demo-controls"),
    page.locator(".demo-active-run"),
    page.locator(".demo-approval"),
    page.locator(".demo-trace"),
    page.locator(".demo-evidence"),
    page.locator(".demo-history"),
  ];
  const positions = await Promise.all(sections.map(verticalTop));
  expect(positions).toEqual([...positions].sort((left, right) => left - right));
  expect(new Set(positions).size).toBe(positions.length);

  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(
    true,
  );
  const transitionDurationMs = await page.locator(".demo-progress i").evaluate((element) => {
    const duration = getComputedStyle(element).transitionDuration;
    return duration.endsWith("ms") ? parseFloat(duration) : parseFloat(duration) * 1_000;
  });
  expect(transitionDurationMs).toBeLessThanOrEqual(1);
});

test("the direct Demo route stays controlled when the feature is disabled", async ({ page }) => {
  await page.goto("/demo?scenario=empty");

  await expect(page.getByRole("heading", { name: "Demo Lab is disabled" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Start Run" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Demo Lab", exact: true })).toHaveCount(0);
});
