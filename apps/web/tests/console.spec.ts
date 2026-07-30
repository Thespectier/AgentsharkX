import { expect, test } from "@playwright/test";

const workspaces = [
  ["/connect/overview", "Connection overview"],
  ["/trust/overview", "Trust overview"],
  ["/protect/overview", "Protection overview"],
  ["/audit/traces", "Agent Trace"],
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

test("Home shows monitored Agent counts and explicit runtime-state availability", async ({
  page,
}) => {
  await page.goto("/");
  const overview = page.getByRole("region", { name: "Agent monitoring overview" });
  await expect(overview.getByText("Monitored agents", { exact: true })).toBeVisible();
  await expect(overview.getByText("3", { exact: true }).first()).toBeVisible();
  await expect(overview.getByText("Running agents", { exact: true })).toBeVisible();
  await expect(overview.getByText("Runtime state is not reported")).toBeVisible();
  await expect(overview.getByText("research-copilot", { exact: true })).toBeVisible();
  await expect(page.getByText("Traffic & decisions", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("img", { name: /requests trend chart for the last 60 minutes/i }),
  ).toBeVisible();
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
  await expect(page.getByRole("link", { name: "系统", exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "信任", exact: true }).click();
  await expect(page.getByRole("heading", { name: "信任概览" })).toBeVisible();
});

test("sidebar subnavigation renders immediately without a hard refresh", async ({ page }) => {
  await page.goto("/connect/overview");
  const connectNavigation = page.getByRole("group", { name: "Connect sections" });
  await expect(connectNavigation).toBeVisible();
  for (const name of ["Overview", "Agents", "LLM / Provider", "MCP / Tools", "Traffic"]) {
    await expect(connectNavigation.getByRole("link", { name, exact: true })).toBeVisible();
  }
  await expect(connectNavigation.getByRole("link", { name: "Overview" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.locator(".workspace-tabs")).toHaveCount(0);
  await connectNavigation.getByRole("link", { name: "LLM / Provider", exact: true }).click();
  await expect(page).toHaveURL(/\/connect\/llm$/);
  await expect(page.getByRole("heading", { name: "Providers" })).toBeVisible();

  await page.goto("/trust/overview");
  const trustNavigation = page.getByRole("group", { name: "Trust sections" });
  await trustNavigation.getByRole("link", { name: "Guardrails", exact: true }).click();
  await expect(page).toHaveURL(/\/trust\/guardrails$/);
  await expect(page.getByRole("heading", { name: "LLM guardrails" })).toBeVisible();

  await page.goto("/protect/overview");
  const protectNavigation = page.getByRole("group", { name: "Protect sections" });
  await protectNavigation.getByRole("link", { name: "Approvals", exact: true }).click();
  await expect(page).toHaveURL(/\/protect\/approvals$/);
  await expect(page.getByRole("heading", { name: "Approvals" })).toBeVisible();

  await page.goto("/audit/traces");
  const auditNavigation = page.getByRole("group", { name: "Audit sections" });
  await auditNavigation.getByRole("link", { name: "Security events", exact: true }).click();
  await expect(page).toHaveURL(/\/audit\/security-events$/);
  await expect(
    page.getByRole("heading", { level: 1, name: "Security Events", exact: true }),
  ).toBeVisible();
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
  await expect(connectNavigation.getByRole("link", { name: "Agents" })).toBeVisible();
});

test("Audit evidence filters have observable behavior", async ({ page }) => {
  await page.goto("/audit/security-events");
  await page.getByRole("button", { name: "Filter" }).click();
  await page.getByPlaceholder("Summary, agent, model, or resource").fill("shell invocation");
  await page.getByLabel("Source").selectOption("agentguard");
  await page.getByLabel("Severity").selectOption("critical");
  await expect(page.locator("tbody tr")).toHaveCount(1);
  await expect(page.locator("tbody tr")).toContainText("shell invocation");
});

test("Audit Traces supports list filters, deterministic flow, and on-demand Span detail", async ({
  page,
}) => {
  await page.goto("/audit/traces");
  await expect(page.getByRole("heading", { level: 1, name: "Agent Trace" })).toBeVisible();
  await page.getByLabel("Filter by A2A").selectOption("true");
  await page.getByRole("button", { name: "Apply" }).click();
  await expect(page).toHaveURL(/has_a2a=true/);

  const traceRow = page.getByRole("row", { name: /task-research-042/ });
  await expect(traceRow).toContainText("1 · observed");
  await traceRow.click();
  await expect(page).toHaveURL(/\/audit\/traces\/11111111111111111111111111111111\?has_a2a=true/);
  await expect(page.getByRole("heading", { level: 1, name: "task-research-042" })).toBeVisible();
  await expect(page.locator(".trace-metrics")).toContainText("LLM calls3");
  await expect(page.locator(".trace-metrics")).toContainText("MCP calls2");
  await expect(page.locator('[data-edge-kind="parent"]')).toHaveCount(7);
  await expect(page.locator('[data-edge-kind="link"]')).toHaveCount(1);
  await expect(page.getByRole("group", { name: "Trace flow lanes" })).toBeVisible();

  await page.getByRole("button", { name: "Open span Plan research" }).click();
  const drawer = page.getByRole("dialog", { name: "Plan research" });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByText("Captured content is available below.")).toBeVisible();
  await expect(drawer.getByText(/Plan the verified inventory research task/)).toBeVisible();
  await expect(page).toHaveURL(/span=2000000000000002/);

  await page.reload();
  await expect(page.getByRole("dialog", { name: "Plan research" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Open span Plan research" })).toHaveClass(
    /trace-flow__node--selected/,
  );
});

test("large Trace rendering remains folded and user-bounded", async ({ page }) => {
  await page.goto("/audit/traces/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee");
  const flow = page.locator(".trace-flow");
  await expect(flow).toBeVisible();
  await expect(flow).toHaveAttribute("data-node-count", /\d+/);
  expect(Number(await flow.getAttribute("data-node-count"))).toBeLessThanOrEqual(48);
  await expect(flow.getByText(/spans folded for clarity/)).toBeVisible();

  await page.getByLabel("TraceFlow node limit").selectOption("24");
  expect(Number(await flow.getAttribute("data-node-count"))).toBeLessThanOrEqual(24);
  await expect(page.getByRole("button", { name: "Show 100 more spans" })).toBeVisible();
});

test("Trace coverage remains readable and contained on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/audit/traces/11111111111111111111111111111111");

  const coverageLabel = page.locator(".trace-coverage strong");
  await expect(coverageLabel).toHaveText("Coverage");
  await expect(coverageLabel).toBeVisible();
  expect(
    await coverageLabel.evaluate((element) => element.scrollWidth <= element.clientWidth),
  ).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );

  await page.goto("/audit/traces");
  const rowDetails = page
    .getByRole("row", { name: /task-research-042/ })
    .locator(".trace-row-details");
  await expect(rowDetails).toBeVisible();
  await expect(rowDetails).toContainText("LLM 3");
  await expect(rowDetails).toContainText("MCP 2");
  await expect(rowDetails).toContainText("Local 1");
  await expect(rowDetails).toContainText("Tokens 1,626");
  await expect(rowDetails).toContainText("Risk low");
});

test("a live Span refreshes its Trace summary without clearing the selected node", async ({
  page,
}) => {
  await page.goto("/audit/traces/22222222222222222222222222222222");

  const selected = page.getByRole("button", { name: "Open span Assess incident" });
  await selected.click();
  await expect(selected).toHaveClass(/trace-flow__node--selected/);
  await expect(page.locator(".trace-flow-controls")).toContainText("3 spans", { timeout: 8_000 });
  await expect(selected).toHaveClass(/trace-flow__node--selected/);
  await expect(page.locator(".trace-metrics")).toContainText("Local tools1");
  await expect(page.getByRole("button", { name: "Open span Live tool update 1" })).toBeVisible();
});

test("Audit Traces exposes empty, partial, missing, forbidden, and database failure states", async ({
  page,
}) => {
  await page.goto("/audit/traces?scenario=empty");
  await expect(page.getByRole("heading", { name: "No traces found" })).toBeVisible();

  await page.goto("/audit/traces?scenario=partial");
  await page.getByRole("row", { name: /task-ops-live/ }).click();
  await expect(page.getByText("Trace is still running")).toBeVisible();
  await expect(page.getByText("No A2A interaction observed")).toBeVisible();

  await page.goto("/audit/traces/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
  await expect(page.getByRole("heading", { level: 1, name: "Trace not found" })).toBeVisible();

  await page.goto("/audit/traces/ffffffffffffffffffffffffffffffff");
  await expect(page.getByRole("heading", { level: 1, name: "Trace access denied" })).toBeVisible();

  await page.goto("/audit/traces?scenario=error");
  await expect(
    page.getByRole("heading", { level: 1, name: "Trace database unavailable" }),
  ).toBeVisible();
});

test("console text uses the enlarged readable scale", async ({ page }) => {
  await page.goto("/connect/llm");
  await expect(page.locator(".data-table").first()).toHaveCSS("font-size", "12px");
});

test("Connect manages verified LLM providers and direct models", async ({ page }) => {
  await page.goto("/connect/overview");
  await expect(page.getByText("Request-log analytics storage is not configured")).toBeVisible();
  await expect(page.getByRole("link", { name: "Raw Config" })).toHaveCount(0);

  await page.goto("/connect/llm");
  await page.getByLabel("Filter providers").fill("openai-shared");
  let row = page.getByRole("row", { name: /openai-shared/ });
  await row.getByRole("button", { name: "Delete provider" }).click();
  let dialog = page.getByRole("dialog");
  await expect(dialog.getByText(/used by a virtual model: fast/)).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Delete", exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await page.getByLabel("Filter providers").fill("");

  await page.getByRole("button", { name: "Add provider" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Provider name").fill("anthropic-backup");
  await dialog.getByLabel("Provider type").selectOption("anthropic");
  await dialog.getByLabel("Credential mode").selectOption("literal");
  await dialog.getByLabel("Provider API key").fill("browser-write-only-key");
  await dialog.getByRole("button", { name: "Save provider" }).click();
  await expect(page.getByText("Provider created in Agentshark Connection.")).toBeVisible();
  await expect(page.getByText("browser-write-only-key")).toHaveCount(0);

  await page.getByRole("button", { name: "Add model" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Model name").fill("backup-chat");
  await dialog.getByLabel("Shared provider").selectOption("anthropic-backup");
  await dialog.getByLabel("Outgoing model").selectOption("explicit");
  await dialog.getByLabel("Explicit outgoing model").fill("claude-haiku-4-5");
  await dialog.getByRole("button", { name: "Save model" }).click();
  await expect(page.getByText("Model created in Agentshark Connection.")).toBeVisible();

  await page.getByLabel("Filter models").fill("backup-chat");
  row = page.getByRole("row", { name: /backup-chat/ });
  await expect(row).toContainText("anthropic-backup");

  await page.getByLabel("Filter providers").fill("anthropic-backup");
  row = page.getByRole("row", { name: /anthropic-backup/ });
  await row.getByRole("button", { name: "Delete provider" }).click();
  dialog = page.getByRole("dialog");
  await expect(dialog.getByText(/also deletes the direct models listed below/)).toBeVisible();
  await expect(dialog.getByText("backup-chat", { exact: true })).toBeVisible();
  await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText("Provider deleted from Agentshark Connection.")).toBeVisible();
  await page.getByLabel("Filter models").fill("backup-chat");
  await expect(page.getByRole("row", { name: /backup-chat/ })).toHaveCount(0);
});

test("Connect manages verified MCP settings and server transports", async ({ page }) => {
  await page.goto("/connect/mcp");
  await expect(page.getByRole("heading", { name: "MCP settings" })).toBeVisible();
  await expect(page.getByRole("row", { name: /filesystem/ })).toContainText("Command line");
  const openapiRow = page.getByRole("row", { name: /catalog-api/ });
  await expect(
    openapiRow.getByRole("button", { name: "OpenAPI targets use advanced configuration" }).first(),
  ).toBeDisabled();
  await expect(page.getByRole("link", { name: "Advanced configuration" })).toHaveCount(0);

  await page.getByRole("button", { name: "Edit MCP settings" }).click();
  let dialog = page.getByRole("dialog", { name: "Edit MCP settings" });
  await dialog.getByLabel("Port").fill("3100");
  await dialog.getByRole("radio", { name: "Stateless" }).click();
  await dialog.getByRole("radio", { name: "Always" }).click();
  await dialog.getByRole("radio", { name: "Fail open" }).click();
  await dialog.getByRole("button", { name: "Save settings" }).click();
  await expect(page.getByText("MCP settings updated")).toBeVisible();
  await expect(page.getByText("3100", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Add server" }).click();
  dialog = page.getByRole("dialog", { name: "Add MCP server" });
  await dialog.getByLabel("Server name").fill("search-tools");
  await dialog.getByRole("radio", { name: "Command line" }).click();
  await dialog.getByLabel("Command").fill("node");
  await dialog.getByLabel("Arguments (JSON array)").fill('["server.js","--tenant","ops"]');
  await dialog
    .getByLabel("Environment (JSON object)")
    .fill('{"ACCESS_TOKEN":"complete-browser-value","LOG_LEVEL":"debug"}');
  await dialog.getByLabel("Clear inherited environment").check();
  await dialog.getByRole("button", { name: "Save server" }).click();
  await expect(page.getByText("MCP server created")).toBeVisible();

  await page.getByLabel("Filter MCP servers").fill("search-tools");
  let row = page.getByRole("row", { name: /search-tools/ });
  await expect(row).toContainText("node server.js --tenant ops");
  await row.getByRole("button", { name: "Edit MCP server" }).click();
  dialog = page.getByRole("dialog", { name: "Edit MCP server" });
  await expect(dialog.getByLabel("Environment (JSON object)")).toContainText(
    "complete-browser-value",
  );
  await dialog.getByRole("button", { name: "Cancel" }).click();

  row = page.getByRole("row", { name: /search-tools/ });
  await row.getByRole("button", { name: "Delete MCP server" }).click();
  dialog = page.getByRole("dialog", { name: "Delete MCP server" });
  await dialog.getByLabel("I understand this target will be removed").check();
  await dialog.getByRole("button", { name: "Delete server" }).click();
  await expect(page.getByText("MCP server deleted")).toBeVisible();
  await expect(page.getByRole("row", { name: /search-tools/ })).toHaveCount(0);
});

test("Connect manages complete listeners and HTTP routes", async ({ page }) => {
  await page.goto("/connect/traffic");
  await expect(page.getByRole("heading", { name: "Traffic listeners" })).toBeVisible();
  await expect(page.getByRole("row", { name: /public-http/ })).toContainText("HTTPS");

  await page.getByRole("button", { name: "Add bind" }).click();
  let dialog = page.getByRole("dialog", { name: "Add bind" });
  await dialog.getByLabel("Port").fill("7070");
  await dialog.getByRole("button", { name: "Save bind" }).click();
  await expect(page.getByText("Traffic bind created")).toBeVisible();

  let bind = page.locator(".traffic-bind").filter({ hasText: ":7070" });
  await bind.getByRole("button", { name: "Add listener" }).click();
  dialog = page.getByRole("dialog", { name: "Add listener" });
  await dialog.getByLabel("Name", { exact: true }).fill("admin-http");
  await dialog.getByLabel("Namespace").fill("default");
  await dialog.getByLabel("Hostname").fill("admin.example.com");
  await dialog.getByLabel("Protocol").selectOption("HTTPS");
  await dialog
    .getByLabel("TLS configuration")
    .fill('{"mode":"static","cert":"/certs/admin.crt","key":"/certs/admin.key"}');
  await dialog
    .getByLabel("Listener policies")
    .fill('{"cors":{"allowOrigins":["https://admin.example.com"]}}');
  await dialog.getByRole("button", { name: "Save listener" }).click();
  await expect(page.getByText("Traffic listener created")).toBeVisible();
  await expect(page.getByRole("row", { name: /admin-http/ })).toContainText("HTTPS");

  await page.getByRole("radio", { name: "Routes" }).click();
  await page.getByRole("button", { name: "Add route" }).click();
  dialog = page.getByRole("dialog", { name: "Add route" });
  await dialog.getByLabel("Listener").selectOption({ label: ":7070 · admin-http · HTTP" });
  await dialog.getByLabel("Name", { exact: true }).fill("admin-api");
  await dialog.getByLabel("Hostnames").fill("admin.example.com");
  await dialog
    .getByLabel("HTTP matches")
    .fill(
      '[{"path":{"pathPrefix":"/api"},"method":"POST","headers":[{"name":"x-role","value":{"exact":"admin"}}],"query":[{"name":"trace","value":{"regex":"1|true"}}]}]',
    );
  await dialog
    .getByLabel("Backend configuration")
    .fill(
      '[{"host":"admin.internal:8443","weight":3,"policies":{"backendTLS":{"insecure":true}}},{"routeGroup":"shared-admin"}]',
    );
  await dialog
    .getByLabel("Route policies")
    .fill('{"timeout":{"requestTimeout":"20s"},"localRateLimit":{"maxTokens":100}}');
  await dialog.getByRole("button", { name: "Save route" }).click();
  await expect(page.getByText("Traffic route created")).toBeVisible();

  let route = page.getByRole("row", { name: /admin-api/ });
  await expect(route).toContainText("POST /api");
  await expect(route).toContainText("2");
  await route.getByRole("button", { name: "Edit route" }).click();
  dialog = page.getByRole("dialog", { name: "Edit route" });
  await expect(dialog.getByLabel("Backend configuration")).toContainText("shared-admin");
  await expect(dialog.getByLabel("Route policies")).toContainText("localRateLimit");
  await dialog.getByRole("button", { name: "Cancel" }).click();

  route = page.getByRole("row", { name: /admin-api/ });
  await route.getByRole("button", { name: "Delete route" }).click();
  dialog = page.getByRole("dialog", { name: "Delete traffic route" });
  await dialog.getByLabel("I understand this configuration will be removed").check();
  await dialog.getByRole("button", { name: "Delete route" }).click();
  await expect(page.getByText("Traffic route deleted")).toBeVisible();

  await page.getByRole("radio", { name: "Listeners" }).click();
  bind = page.locator(".traffic-bind").filter({ hasText: ":7070" });
  await bind.getByRole("button", { name: "Delete bind" }).click();
  dialog = page.getByRole("dialog", { name: "Delete traffic bind" });
  await dialog.getByLabel("I understand this configuration will be removed").check();
  await dialog.getByRole("button", { name: "Delete bind" }).click();
  await expect(page.getByText("Traffic bind deleted")).toBeVisible();
  await expect(page.locator(".traffic-bind").filter({ hasText: ":7070" })).toHaveCount(0);
});

test("Trust requires a current syntax check and returns rule mutation receipts", async ({
  page,
}) => {
  await page.goto("/trust/runtime-rules");
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

test("Trust manages complete LLM, model, and MCP policy values", async ({ page }) => {
  await page.goto("/trust/policy");
  await expect(page.getByRole("tab", { name: "LLM / MODEL" })).toHaveAttribute(
    "aria-selected",
    "true",
  );

  let row = page.getByRole("row", { name: /Basic auth/ });
  await row.getByRole("button", { name: "Configure policy: Basic auth" }).click();
  let dialog = page.getByRole("dialog", { name: "Configure Basic auth" });
  const editor = dialog.getByLabel(/Complete policy value/);
  await editor.fill("{invalid");
  await dialog.getByRole("button", { name: "Save policy" }).click();
  await expect(dialog.getByRole("alert")).toBeVisible();
  await editor.fill('{"users":{"admin":"$2y$mock"}}');
  await dialog.getByRole("button", { name: "Save policy" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Gateway policy saved" })).toBeVisible();
  await expect(row).toContainText("Enabled");

  await row.getByRole("button", { name: "Edit policy: Basic auth" }).click();
  dialog = page.getByRole("dialog", { name: "Edit Basic auth" });
  await expect(dialog.getByLabel(/Complete policy value/)).toContainText("$2y$mock");
  await dialog.getByLabel(/Complete policy value/).fill('{"users":{"admin":"$2y$updated"}}');
  await dialog.getByRole("button", { name: "Save policy" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Gateway policy saved" })).toBeVisible();

  row = page.getByRole("row", { name: /Request overrides/ });
  await row.getByRole("button", { name: "Configure policy: Request overrides" }).click();
  dialog = page.getByRole("dialog", { name: "Configure Request overrides" });
  await dialog.getByLabel(/Complete policy value/).fill('{"stream":false,"temperature":0.1}');
  await dialog.getByRole("button", { name: "Save policy" }).click();
  await expect(row).toContainText("Enabled");

  await page.getByRole("tab", { name: "MCP", exact: true }).click();
  await expect(page.getByRole("tab", { name: "MCP", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  row = page.getByRole("row", { name: /MCP authentication/ });
  await row.getByRole("button", { name: "Configure policy: MCP authentication" }).click();
  dialog = page.getByRole("dialog", { name: "Configure MCP authentication" });
  await dialog
    .getByLabel(/Complete policy value/)
    .fill('{"issuer":"https://identity.example","audiences":["tools"]}');
  await dialog.getByRole("button", { name: "Save policy" }).click();
  await expect(row).toContainText("Enabled");

  await row.getByRole("button", { name: "Delete policy: MCP authentication" }).click();
  const deletion = page.getByRole("dialog", { name: "Delete gateway policy" });
  await deletion.getByLabel("I confirm this connection policy should be removed.").check();
  await deletion.getByRole("button", { name: "Delete policy" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Gateway policy deleted" }),
  ).toBeVisible();
  await expect(row).toContainText("Disabled");
});

test("Trust policy manager stays contained on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/trust/policy");
  await expect(page.getByRole("tab", { name: "LLM / MODEL" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page
    .getByRole("row", { name: /Basic auth/ })
    .getByRole("button", { name: "Configure policy: Basic auth" })
    .click();
  const dialog = page.getByRole("dialog", { name: "Configure Basic auth" });
  const bounds = await dialog.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const footer = element.querySelector("footer")?.getBoundingClientRect();
    return {
      contained:
        rect.left >= 0 && rect.right <= window.innerWidth && rect.bottom <= window.innerHeight,
      footerContained: Boolean(
        footer &&
        footer.left >= rect.left &&
        footer.right <= rect.right &&
        footer.bottom <= rect.bottom,
      ),
      horizontalOverflow: element.scrollWidth > element.clientWidth,
    };
  });
  expect(bounds).toEqual({ contained: true, footerContained: true, horizontalOverflow: false });
});

test("runtime rule composer stays contained and clears stale publication state", async ({
  page,
}) => {
  await page.setViewportSize({ width: 800, height: 700 });
  await page.goto("/trust/runtime-rules");
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
    "Runtime protection",
  );
  await expect(page.getByRole("heading", { name: /agents are in control/i })).toBeVisible();

  await page.goto("/?scenario=error");
  await expect(page.getByRole("heading", { name: "Control plane unavailable" })).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("All sources are unavailable");
});

test("an audit detail drawer is recoverable from its URL", async ({ page }) => {
  await page.goto("/audit/security-events");
  const title = "Runtime rule blocked a shell invocation outside the workspace boundary.";
  const row = page.locator("tbody tr").filter({ hasText: title });
  await expect(row).toHaveCount(1);
  await row.click();
  await expect(page).toHaveURL(/\/audit\/security-events\?event=/);
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("heading", { level: 2, name: title })).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  const selectedRow = page
    .locator("tbody tr")
    .filter({ has: page.getByText(title, { exact: true }) })
    .first();
  await expect(selectedRow).toBeFocused();
  await selectedRow.click();

  await page.reload();
  await expect(
    page.getByRole("dialog").getByRole("heading", { level: 2, name: title }),
  ).toBeVisible();
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

test("audit detail exposes the complete BFF payload without an upstream jump link", async ({
  page,
}) => {
  await page.goto("/audit/traffic");
  await page.getByText("Chat completion routed through the primary OpenAI backend.").click();

  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("link", { name: "Open exact source log" })).toHaveCount(0);
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

  await page.goto("/audit/traffic");
  await expect(page.getByText(/^\[Mock live\]/).first()).toBeVisible({ timeout: 5_000 });
});

test("the command palette supports keyboard navigation", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: /agents are in control/i })).toBeVisible();
  await page.keyboard.press("Control+k");
  const input = page.getByRole("combobox", { name: "Search commands" });
  await expect(input).toBeFocused();
  await input.fill("Open Trust");
  await input.press("Enter");
  await expect(page).toHaveURL(/\/trust\/overview$/);
  await expect(page.getByRole("heading", { name: "Trust overview" })).toBeVisible();
});

test("reduced motion removes continuous animation", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  await expect(page.locator("animateMotion")).toHaveCount(0);
  const ambientAnimation = await page.evaluate(
    () => getComputedStyle(document.body, "::before").animationName,
  );
  expect(ambientAnimation).toBe("none");
});

test("navigation exposes only Agentshark product language and internal destinations", async ({
  page,
}) => {
  for (const path of [
    "/",
    "/connect/overview",
    "/connect/llm",
    "/connect/mcp",
    "/trust/overview",
    "/trust/guardrails",
    "/protect/overview",
    "/audit/traces",
  ]) {
    await page.goto(path);
    await expect(page.locator("body")).not.toContainText(/AgentGateway|AgentGuard/i);
    await expect(page.getByRole("link", { name: "System" })).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Documentation" })).toHaveCount(0);
    expect(
      await page.locator("a").evaluateAll((links) =>
        links.every((link) => {
          const href = link.getAttribute("href") ?? "";
          return !/^https?:\/\//i.test(href);
        }),
      ),
    ).toBe(true);
  }
});
