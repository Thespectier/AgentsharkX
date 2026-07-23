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

test("workspace tabs render immediately without a hard refresh", async ({ page }) => {
  await page.goto("/connect/overview");
  await page.getByRole("link", { name: "LLM", exact: true }).click();
  await expect(page).toHaveURL(/\/connect\/llm$/);
  await expect(page.getByRole("heading", { name: "Providers" })).toBeVisible();

  await page.goto("/trust/agents");
  await page.getByRole("link", { name: "Resources", exact: true }).click();
  await expect(page).toHaveURL(/\/trust\/resources$/);
  await expect(page.getByRole("heading", { name: "Runtime resources" })).toBeVisible();

  await page.goto("/protect/policies");
  await page.getByRole("link", { name: "Guardrails", exact: true }).click();
  await expect(page).toHaveURL(/\/protect\/guardrails$/);
  await expect(page.getByRole("heading", { name: "Content guardrails" })).toBeVisible();

  await page.goto("/audit/analytics");
  await page.getByRole("link", { name: "Security events", exact: true }).click();
  await expect(page).toHaveURL(/\/audit\/security-events$/);
  await expect(page.getByRole("heading", { name: "Security events" })).toBeVisible();
});

test("interactive controls have observable behavior", async ({ page }) => {
  await page.goto("/audit/analytics");
  await page.getByRole("button", { name: "Filter" }).click();
  await page.getByPlaceholder("Summary, agent, model, or resource").fill("shell invocation");
  await page.getByLabel("Source").selectOption("agentguard");
  await page.getByLabel("Severity").selectOption("critical");
  await expect(page.locator("tbody tr")).toHaveCount(1);
  await expect(page.locator("tbody tr")).toContainText("shell invocation");

  await page.getByRole("link", { name: "Open system settings" }).click();
  await expect(page).toHaveURL(/\/system$/);
  await expect(
    page.getByRole("heading", { name: "Sources, versions & capabilities" }),
  ).toBeVisible();
});

test("configuration entry points target both native control planes", async ({ page }) => {
  await page.goto("/connect/overview");
  await expect(page.getByRole("link", { name: "Configure agentgateway" })).toHaveAttribute(
    "href",
    "http://localhost:15000/ui/raw-config",
  );

  await page.goto("/protect/policies");
  await expect(page.getByRole("link", { name: "Configure AgentGuard" })).toHaveAttribute(
    "href",
    "http://localhost:38008",
  );
});

test("console text uses the enlarged readable scale", async ({ page }) => {
  await page.goto("/connect/llm");
  await expect(page.locator(".data-table").first()).toHaveCSS("font-size", "12px");
  await expect(page.getByRole("link", { name: "Configure agentgateway" })).toHaveCSS(
    "font-size",
    "13px",
  );
});

test("Connect filters explicit resources, opens details, and reruns setup verification", async ({
  page,
}) => {
  await page.goto("/connect/overview");
  await expect(page.getByText("Request-log analytics storage is not configured")).toBeVisible();
  await expect(page.getByRole("link", { name: "Raw Config" })).toHaveAttribute(
    "href",
    "http://localhost:15000/ui/raw-config",
  );

  await page.goto("/connect/llm");
  const modelFilter = page.getByPlaceholder("Filter explicit resources").nth(1);
  await modelFilter.fill("fast");
  const modelRow = page.getByRole("row", { name: /fast/ });
  await expect(modelRow).toBeVisible();
  await modelRow.click();
  const drawer = page.getByRole("dialog");
  await expect(drawer.getByRole("heading", { level: 2 })).toHaveText("fast");
  await expect(drawer).toContainText("/mock/fast");
  await page.keyboard.press("Escape");

  await page.goto("/connect/setup");
  await expect(page.getByText("Connection verified")).toBeVisible();
  await page.getByRole("button", { name: "Run check" }).click();
  await expect(page.getByText("Connection verified")).toBeVisible();
});

test("Trust uses explicit identities, confirms labels, and recovers a polled scan", async ({
  page,
}) => {
  await page.goto("/trust/agents");
  await page.getByPlaceholder("Filter explicit Trust data").fill("research-copilot");
  const agentRow = page.getByRole("row", { name: /research-copilot/ });
  await expect(agentRow).toBeVisible();
  await agentRow.click();
  const workspace = page.getByRole("dialog");
  await expect(
    workspace.getByRole("heading", { name: "research-copilot", level: 2 }),
  ).toBeVisible();
  await expect(workspace).toContainText("agent_id:research-copilot");
  await page.keyboard.press("Escape");

  await page.goto("/trust/resources");
  await page.getByRole("button", { name: "Edit labels for send_email_to" }).click();
  const labels = page.getByRole("dialog");
  await labels.getByLabel("Boundary").fill("internet");
  await labels.getByRole("button", { name: "Save labels" }).click();
  await expect(labels).toContainText("Saving labels…");
  await expect(labels).toBeHidden();
  await expect(page.getByRole("row", { name: /send_email_to/ })).toContainText("server-confirmed");

  await page.goto("/trust/resources?scenario=partial");
  await page.getByRole("button", { name: "Scan web-research" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Detection running" })).toBeVisible();
  page.once("dialog", async (dialog) => {
    expect(dialog.message()).toContain("detection is still running");
    await dialog.dismiss();
  });
  await page.getByRole("link", { name: "Agents" }).click();
  await expect(page).toHaveURL(/\/trust\/resources/);
  await expect(page.getByRole("alert").filter({ hasText: "Detection failed" })).toBeVisible();
  await page.getByRole("button", { name: "Retry scan" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Detection succeeded" })).toBeVisible();
});

test("Protect requires a current syntax check and returns rule mutation receipts", async ({
  page,
}) => {
  await page.goto("/protect/runtime-rules");
  await page.getByRole("button", { name: "New rule" }).click();
  const dialog = page.getByRole("dialog", { name: "Publish runtime rule" });
  const publish = dialog.getByRole("button", { name: "Publish checked rule" });
  await expect(publish).toBeDisabled();

  await dialog.getByRole("button", { name: "Check syntax" }).click();
  await expect(dialog.getByRole("status")).toContainText("Checked and publishable");
  await dialog.getByLabel("Rule source").fill("RULE: changed_rule\nPOLICY: DENY");
  await expect(dialog.getByText("Check required before publish")).toBeVisible();
  await expect(publish).toBeDisabled();

  await dialog.getByRole("button", { name: "Check syntax" }).click();
  await expect(dialog.getByRole("status")).toContainText("Checked and publishable");
  await dialog.getByLabel("Operator note").fill("Reviewed for the active change window.");
  await dialog
    .getByLabel("I confirm this checked rule should be published to the selected agent.")
    .check();
  await publish.click();
  await expect(publish).toBeDisabled();
  await expect(
    page.getByRole("status").filter({ hasText: "Runtime rule published" }),
  ).toContainText("Request ID");

  await page.getByRole("button", { name: "Delete New checked runtime rule" }).click();
  const deletion = page.getByRole("dialog", { name: "Delete New checked runtime rule" });
  await deletion.getByLabel("Deletion note").fill("Superseded after verification.");
  await deletion.getByLabel("I confirm this runtime rule should be deleted.").check();
  await deletion.getByRole("button", { name: "Delete rule" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Runtime rule deleted" })).toContainText(
    "Request ID",
  );
});

test("Protect approval success and upstream 404 are explicit and recoverable", async ({ page }) => {
  await page.goto("/protect/approvals");
  await expect(page.getByRole("link", { name: "3 pending approvals" })).toBeVisible();
  await page.getByRole("button", { name: /send_email_to/ }).click();
  let dialog = page.getByRole("dialog", { name: "Review send_email_to" });
  await dialog.getByLabel("Operator note").fill("Validated destination and change owner.");
  await dialog
    .getByLabel("I confirm this operator decision for the selected pending ticket.")
    .check();
  const approve = dialog.getByRole("button", { name: "Approve", exact: true });
  await approve.click();
  await expect(approve).toBeDisabled();
  await expect(
    page.getByRole("status").filter({ hasText: "Approval ticket approved" }),
  ).toContainText("Request ID");
  await expect(page.getByRole("link", { name: "2 pending approvals" })).toBeVisible();

  await page.getByRole("button", { name: /deploy.restart/ }).click();
  dialog = page.getByRole("dialog", { name: "Review deploy.restart" });
  await dialog.getByLabel("Operator note").fill("Ticket state needs verification.");
  await dialog
    .getByLabel("I confirm this operator decision for the selected pending ticket.")
    .check();
  await dialog.getByRole("button", { name: "Deny", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("no longer pending");
  await expect(dialog.getByRole("alert")).toContainText("Request ID");
});

test("Protect approval timeout is never auto-retried and supports a manual retry", async ({
  page,
}) => {
  await page.goto("/protect/approvals?scenario=partial");
  await page.getByRole("button", { name: /crm.update_contact/ }).click();
  const dialog = page.getByRole("dialog", { name: "Review crm.update_contact" });
  await dialog.getByLabel("Operator note").fill("Reviewed for an explicit retry.");
  await dialog
    .getByLabel("I confirm this operator decision for the selected pending ticket.")
    .check();
  await dialog.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("timed out");
  const retry = dialog.getByRole("button", { name: "Retry approve" });
  await expect(retry).toBeEnabled();
  await retry.click();
  await expect(
    page.getByRole("status").filter({ hasText: "Approval ticket approved" }),
  ).toContainText("Request ID");
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
  const selectedRow = page
    .locator("tbody tr")
    .filter({ has: page.getByText(title ?? "", { exact: true }) })
    .first();
  await expect(selectedRow).toBeFocused();
  await selectedRow.click();

  await page.reload();
  await expect(page.getByRole("dialog").getByRole("heading", { level: 2 })).toHaveText(title ?? "");
});

test("real-time events reach Home and Audit within three seconds", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText(/^\[Mock live\]/).first()).toBeVisible({ timeout: 3_000 });

  await page.goto("/audit/analytics");
  await expect(page.getByText(/^\[Mock live\]/).first()).toBeVisible({ timeout: 3_000 });
});

test("hidden documents pause LiveFlow while retaining incoming data", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText(/^\[Mock live\]/).first()).toBeVisible({ timeout: 3_000 });
  const firstSummary = await page.locator(".activity-item p").first().textContent();

  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(page.locator(".live-flow")).toHaveAttribute("data-motion", "paused");
  await expect(page.locator("animateMotion")).toHaveCount(0);
  await page.waitForTimeout(4_200);

  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => false });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(page.locator(".live-flow")).toHaveAttribute("data-motion", "full");
  await expect(page.locator(".activity-item p").first()).not.toHaveText(firstSummary ?? "", {
    timeout: 1_500,
  });
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
