import { expect, test } from "@playwright/test";

const workspaces = [
  ["/connect/overview", "Connect agents to every destination"],
  ["/trust/agents", "Know what every agent can reach"],
  ["/protect/policies", "Enforce every critical boundary"],
  ["/audit/analytics", "See every verified signal"],
] as const;

test("all five primary pages render from labelled mock data", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: /Your agents/ })).toBeVisible();
  await expect(page.getByText("MOCK DATA")).toBeVisible();
  for (const [path, heading] of workspaces) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
    await expect(page.getByText("MOCK DATA")).toBeVisible();
  }
});

test("Home greeting follows Beijing time and the persisted language switch localizes the shell", async ({
  page,
}) => {
  await page.clock.setFixedTime(new Date("2026-07-21T00:00:00Z"));
  await page.goto("/");
  await expect(
    page.getByRole("heading", {
      level: 1,
      name: "Good morning. Your agents are in control.",
    }),
  ).toBeVisible();
  await expect(page.getByText(/UTC\+8/).first()).toBeVisible();

  await page.getByRole("button", { name: "Switch to Chinese" }).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
  await expect(
    page.getByRole("heading", { level: 1, name: "早上好。您的智能体均在掌控之中。" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "连接", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "切换到英文" })).toBeVisible();

  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
  await expect(page.getByRole("link", { name: "系统", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "系统", exact: true }).click();
  await expect(page.getByRole("heading", { name: "数据源、版本与能力" })).toBeVisible();
});

test("sidebar subnavigation renders immediately without a hard refresh", async ({ page }) => {
  await page.goto("/connect/overview");
  const connectNavigation = page.getByRole("group", { name: "Connect sections" });
  await expect(connectNavigation).toBeVisible();
  for (const name of ["Overview", "LLM", "MCP", "Traffic", "Setup"]) {
    await expect(connectNavigation.getByRole("link", { name, exact: true })).toBeVisible();
  }
  await expect(connectNavigation.getByRole("link", { name: "Overview" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.locator(".workspace-tabs")).toHaveCount(0);
  await connectNavigation.getByRole("link", { name: "LLM", exact: true }).click();
  await expect(page).toHaveURL(/\/connect\/llm$/);
  await expect(page.getByRole("heading", { name: "Providers" })).toBeVisible();

  await page.goto("/trust/agents");
  const trustNavigation = page.getByRole("group", { name: "Trust sections" });
  await trustNavigation.getByRole("link", { name: "Resources", exact: true }).click();
  await expect(page).toHaveURL(/\/trust\/resources$/);
  await expect(page.getByRole("heading", { name: "Runtime resources" })).toBeVisible();

  await page.goto("/protect/policies");
  const protectNavigation = page.getByRole("group", { name: "Protect sections" });
  await protectNavigation.getByRole("link", { name: "Guardrails", exact: true }).click();
  await expect(page).toHaveURL(/\/protect\/guardrails$/);
  await expect(page.getByRole("heading", { name: "Content guardrails" })).toBeVisible();

  await page.goto("/audit/analytics");
  const auditNavigation = page.getByRole("group", { name: "Audit sections" });
  await auditNavigation.getByRole("link", { name: "Security events", exact: true }).click();
  await expect(page).toHaveURL(/\/audit\/security-events$/);
  await expect(page.getByRole("heading", { name: "Security events" })).toBeVisible();
});

test("desktop sidebar collapses into a compact icon rail and restores", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.addInitScript(() => {
    if (localStorage.getItem("agentshark.sidebar") === null) {
      localStorage.setItem("agentshark.sidebar", "expanded");
    }
  });
  await page.goto("/connect/overview");

  const sidebar = page.locator(".sidebar");
  const frame = page.locator(".app-frame");
  await expect(sidebar).toHaveCSS("width", "248px");
  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  await expect(sidebar).toHaveCSS("width", "64px");
  await expect(frame).toHaveCSS("margin-left", "64px");
  await expect(page.getByRole("button", { name: "Expand sidebar" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await expect(page.getByRole("link", { name: "Connect", exact: true })).toHaveAttribute(
    "title",
    "Connect",
  );
  await expect(page.getByRole("group", { name: "Connect sections" })).toBeHidden();

  const rail = await sidebar.evaluate((element) => {
    const links = [...element.querySelectorAll<HTMLElement>(".nav-item")];
    return {
      horizontalOverflow: element.scrollWidth > element.clientWidth,
      linksCentered: links.every((link) => {
        const icon = link.querySelector("svg");
        if (!icon) return false;
        const linkBounds = link.getBoundingClientRect();
        const iconBounds = icon.getBoundingClientRect();
        return (
          Math.abs(
            iconBounds.left + iconBounds.width / 2 - (linkBounds.left + linkBounds.width / 2),
          ) < 1
        );
      }),
    };
  });
  expect(rail).toEqual({ horizontalOverflow: false, linksCentered: true });

  await page.reload();
  await expect(sidebar).toHaveCSS("width", "64px");
  await page.getByRole("button", { name: "Expand sidebar" }).click();
  await expect(sidebar).toHaveCSS("width", "248px");
  await expect(frame).toHaveCSS("margin-left", "248px");
  await expect(page.getByRole("group", { name: "Connect sections" })).toBeVisible();
});

test("mobile navigation exposes subpages even after desktop sidebar was collapsed", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => localStorage.setItem("agentshark.sidebar", "collapsed"));
  await page.goto("/connect/overview");
  await page.getByRole("button", { name: "Open navigation" }).click();
  const connectNavigation = page.getByRole("group", { name: "Connect sections" });
  await expect(connectNavigation).toBeVisible();
  await expect(connectNavigation.getByRole("link", { name: "Setup" })).toBeVisible();
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

test("Connect manages verified LLM providers and direct models, then reruns setup verification", async ({
  page,
}) => {
  await page.goto("/connect/overview");
  await expect(page.getByText("Request-log analytics storage is not configured")).toBeVisible();
  await expect(page.getByRole("link", { name: "Raw Config" })).toHaveAttribute(
    "href",
    "http://localhost:15000/ui/raw-config",
  );

  await page.goto("/connect/llm");
  await page.getByRole("button", { name: "Add provider" }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByLabel("Provider name").fill("anthropic-backup");
  await dialog.getByLabel("Provider type").selectOption("anthropic");
  await dialog.getByLabel("Credential mode").selectOption("literal");
  await dialog.getByLabel("Provider API key").fill("browser-write-only-key");
  await dialog.getByRole("button", { name: "Save provider" }).click();
  await expect(page.getByText("Provider created in agentgateway.")).toBeVisible();
  await expect(page.getByText("browser-write-only-key")).toHaveCount(0);

  await page.getByRole("button", { name: "Add model" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Model name").fill("backup-chat");
  await dialog.getByLabel("Shared provider").selectOption("anthropic-backup");
  await dialog.getByLabel("Outgoing model").selectOption("explicit");
  await dialog.getByLabel("Explicit outgoing model").fill("claude-haiku-4-5");
  await dialog.getByRole("button", { name: "Save model" }).click();
  await expect(page.getByText("Model created in agentgateway.")).toBeVisible();

  await page.getByLabel("Filter models").fill("backup-chat");
  let row = page.getByRole("row", { name: /backup-chat/ });
  await expect(row).toContainText("anthropic-backup");
  await row.getByRole("button", { name: "Delete model" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText("Model deleted from agentgateway.")).toBeVisible();

  await page.getByLabel("Filter providers").fill("anthropic-backup");
  row = page.getByRole("row", { name: /anthropic-backup/ });
  await row.getByRole("button", { name: "Delete provider" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText("Provider deleted from agentgateway.")).toBeVisible();

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

test("runtime rule composer stays contained and clears stale publication state", async ({
  page,
}) => {
  await page.setViewportSize({ width: 800, height: 700 });
  await page.goto("/protect/runtime-rules");
  await page.getByRole("button", { name: "New rule" }).click();
  const dialog = page.getByRole("dialog", { name: "Publish runtime rule" });
  const layout = await dialog.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    const footerBounds = element.querySelector("footer")?.getBoundingClientRect();
    const confirmationBounds = element.querySelector(".confirm-field")?.getBoundingClientRect();
    return {
      actionsContained: Boolean(
        footerBounds &&
        footerBounds.left >= bounds.left &&
        footerBounds.right <= bounds.right &&
        footerBounds.top >= bounds.top &&
        footerBounds.bottom <= bounds.bottom,
      ),
      confirmationVisible: Boolean(
        confirmationBounds &&
        footerBounds &&
        confirmationBounds.top >= bounds.top &&
        confirmationBounds.bottom <= footerBounds.top,
      ),
      contained:
        bounds.left >= 0 &&
        bounds.right <= window.innerWidth &&
        bounds.top >= 0 &&
        bounds.bottom <= window.innerHeight,
      horizontallyScrollable: element.scrollWidth > element.clientWidth,
    };
  });
  expect(layout).toEqual({
    actionsContained: true,
    confirmationVisible: true,
    contained: true,
    horizontallyScrollable: false,
  });

  await dialog.getByRole("button", { name: "Check syntax" }).click();
  await expect(dialog.getByRole("status")).toContainText("Checked and publishable");
  await dialog.getByLabel("Operator note").fill("Draft note that must not survive cancellation.");
  await dialog
    .getByLabel("I confirm this checked rule should be published to the selected agent.")
    .check();
  await dialog.getByRole("button", { name: "Cancel" }).click();

  await page.getByRole("button", { name: "New rule" }).click();
  await expect(dialog.getByText("Check required before publish")).toBeVisible();
  await expect(dialog.getByLabel("Operator note")).toHaveValue("");
  await expect(
    dialog.getByLabel("I confirm this checked rule should be published to the selected agent."),
  ).not.toBeChecked();
  await expect(dialog.getByRole("button", { name: "Publish checked rule" })).toBeDisabled();
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

test("audit traffic and security tables show the full Beijing calendar date", async ({ page }) => {
  for (const section of ["traffic", "security-events"] as const) {
    await page.goto(`/audit/${section}`);
    const timestamp = page.locator("tbody tr").first().locator("time");
    await expect(timestamp).toBeVisible();
    await expect(timestamp).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} UTC\+8$/);
    await expect(timestamp).toHaveAttribute("datetime", /^\d{4}-\d{2}-\d{2}T/);
  }
});

test("gateway audit detail exposes complete BFF payload and exact native log", async ({ page }) => {
  await page.goto("/audit/traffic");
  await page.getByText("Chat completion routed through the primary OpenAI backend.").click();

  const sourceLog = page.getByRole("dialog").getByRole("link", {
    name: "Open exact source log",
  });
  await expect(sourceLog).toHaveAttribute(
    "href",
    "http://127.0.0.1:15000/ui/llm/logs?log=log-73b1",
  );
  await expect(sourceLog).toHaveAttribute("target", "_blank");
  const dialog = page.getByRole("dialog");
  const requestPrompt = dialog.getByText("Request prompt", { exact: true }).locator("../..");
  const responseCompletion = dialog
    .getByText("Response completion", { exact: true })
    .locator("../..");
  const attributes = dialog.getByText("Attributes", { exact: true }).locator("../..");
  await expect(requestPrompt).toContainText("Show the full retained request.");
  await expect(responseCompletion).toContainText("This is the complete retained completion.");
  await expect(attributes).toContainText("Bearer synthetic-mock-value");
  await expect(dialog.getByText("Complete upstream JSON", { exact: true })).toBeVisible();
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
