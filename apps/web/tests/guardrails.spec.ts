import { expect, test } from "@playwright/test";

test("Protect manages complete LLM guardrails with structured and JSON controls", async ({
  page,
}) => {
  await page.goto("/protect/guardrails");

  await expect(page.getByRole("heading", { name: "LLM guardrails" })).toBeVisible();
  await expect(page.getByText("/llm/policies/guardrails", { exact: true })).toBeVisible();
  await expect(page.getByText("Regex rules", { exact: true })).toBeVisible();
  await expect(page.getByText("Webhook", { exact: true })).toBeVisible();
  await expect(page.getByText("Azure Content Safety", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Edit guardrails" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Guardrails" });
  await expect(dialog).toBeVisible();

  const requestGuards = dialog.getByRole("group", { name: "Request guards" });
  await requestGuards.getByLabel("Guard type").last().selectOption("openAIModeration");
  await requestGuards.getByRole("button", { name: "Add guard" }).click();
  await requestGuards.getByLabel("Moderation model").fill("omni-moderation-latest");

  await dialog.getByRole("button", { name: "Complete JSON" }).click();
  const jsonEditor = dialog.getByLabel("Complete guardrail value");
  const value = JSON.parse(await jsonEditor.inputValue()) as {
    streaming: string;
    request: unknown[];
    response: unknown[];
  };
  expect(value.request).toHaveLength(3);
  value.streaming = "Disabled";
  value.response.push({
    regex: { action: "reject", rules: [{ builtin: "creditCard" }] },
  });
  await jsonEditor.fill(JSON.stringify(value, null, 2));
  await dialog.getByRole("button", { name: "Save guardrails" }).click();

  await expect(page.getByText("Gateway policy saved")).toBeVisible();
  await expect(page.getByText("Streaming disabled")).toBeVisible();
  const responseSummary = page.locator(".gateway-guardrail-phase").filter({
    hasText: "Response guards",
  });
  await expect(responseSummary.getByText("Regex rules", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Edit guardrails" }).click();
  const reopened = page.getByRole("dialog", { name: "Edit Guardrails" });
  await reopened.getByRole("button", { name: "Complete JSON" }).click();
  await expect(reopened.getByLabel("Complete guardrail value")).toHaveValue(/x-guardrail-result/);
  await expect(reopened.getByLabel("Complete guardrail value")).toHaveValue(/forwardHeaderMatches/);
});

test("Protect manages ordered MCP guardrail processors and confirmed deletion", async ({
  page,
}) => {
  await page.goto("/protect/guardrails");
  await page.getByRole("tab", { name: "MCP" }).click();

  await expect(page.getByRole("heading", { name: "MCP guardrails" })).toBeVisible();
  await expect(page.getByText("/mcp/policies/mcpGuardrails", { exact: true })).toBeVisible();
  await expect(page.getByText("guardrail-backend", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Edit guardrails" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit MCP guardrails" });
  const initialProcessor = dialog.locator(".gateway-guardrail-processor-card").first();
  const secondMethodKey = initialProcessor.getByLabel("Method phases key 2");
  await secondMethodKey.fill("tools/call");
  await secondMethodKey.press("Tab");
  await expect(dialog.getByText("Keys must be unique.")).toBeVisible();
  await expect(secondMethodKey).toHaveValue("tools/*");

  await dialog.getByRole("button", { name: "Add processor" }).click();
  const processors = dialog.locator(".gateway-guardrail-processor-card");
  await expect(processors).toHaveCount(2);
  await expect(processors.nth(0).getByRole("button", { name: "Move processor up" })).toBeDisabled();
  await expect(
    processors.nth(1).getByRole("button", { name: "Move processor down" }),
  ).toBeDisabled();
  await processors.nth(1).getByLabel("Processor host").fill("review.example.invalid:9000");
  await processors.nth(1).getByRole("button", { name: "Add metadata" }).click();
  await processors.nth(1).getByLabel("CEL metadata key 1").fill("principal");
  await processors.nth(1).getByLabel("CEL metadata value 1").fill("jwt.sub");
  await processors
    .nth(1)
    .getByLabel("Allowed request headers", { exact: true })
    .fill("authorization, x-tenant");
  await processors.nth(1).getByRole("button", { name: "Move processor up" }).click();
  await dialog.getByRole("button", { name: "Save guardrails" }).click();

  await expect(page.getByText("Gateway policy saved")).toBeVisible();
  await expect(page.locator(".gateway-guardrail-summary-list--processors li")).toHaveCount(2);
  await expect(
    page.locator(".gateway-guardrail-summary-list--processors li").first(),
  ).toContainText("review.example.invalid:9000");

  await page.getByRole("button", { name: "Edit guardrails" }).click();
  const reopened = page.getByRole("dialog", { name: "Edit MCP guardrails" });
  await reopened.getByRole("button", { name: "Complete JSON" }).click();
  const completeValue = reopened.getByLabel("Complete guardrail value");
  const saved = JSON.parse(await completeValue.inputValue()) as {
    processors: Array<{ host?: string; backend?: string }>;
  };
  expect(saved.processors[0]?.host).toBe("review.example.invalid:9000");
  expect(saved.processors[1]?.backend).toBe("guardrail-backend");
  await expect(completeValue).toHaveValue(/"backend": "guardrail-backend"/);
  await expect(completeValue).toHaveValue(/"requestHeaders"/);
  await expect(completeValue).toHaveValue(/"policies"/);
  await reopened.getByRole("button", { name: "Cancel" }).click();

  await page.getByRole("button", { name: "Delete guardrails: MCP guardrails" }).click();
  const deleteDialog = page.getByRole("dialog", { name: "Delete gateway guardrails" });
  await deleteDialog
    .getByLabel("I confirm this complete agentgateway guardrail configuration should be removed.")
    .check();
  await deleteDialog.getByRole("button", { name: "Delete guardrails" }).click();
  await expect(page.getByText("Guardrails are disabled")).toBeVisible();
  await expect(page.getByText("Gateway policy deleted")).toBeVisible();

  await page.getByRole("button", { name: "Configure guardrails" }).first().click();
  const createDialog = page.getByRole("dialog", { name: "Configure MCP guardrails" });
  await createDialog.getByLabel("Processor host").fill("guardrail.example.invalid:9000");
  await createDialog.getByRole("button", { name: "Save guardrails" }).click();
  await expect(page.getByText("Gateway policy saved")).toBeVisible();
  await expect(page.getByText("guardrail.example.invalid:9000", { exact: true })).toBeVisible();
});

test("Guardrail JSON validation rejects request-only guards in the response phase", async ({
  page,
}) => {
  await page.goto("/protect/guardrails");
  await page.getByRole("button", { name: "Edit guardrails" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Guardrails" });
  await dialog.getByRole("button", { name: "Complete JSON" }).click();
  await dialog
    .getByLabel("Complete guardrail value")
    .fill(JSON.stringify({ response: [{ openAIModeration: {} }] }, null, 2));
  await dialog.getByRole("button", { name: "Save guardrails" }).click();
  await expect(
    dialog.getByText("OpenAI moderation is available only for request guards."),
  ).toBeVisible();
});

test("An open Guardrail draft cannot borrow a newer background revision", async ({ page }) => {
  await page.goto("/protect/guardrails");
  await page.getByRole("button", { name: "Edit guardrails" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Guardrails" });
  await dialog.getByRole("button", { name: "Complete JSON" }).click();
  const jsonEditor = dialog.getByLabel("Complete guardrail value");
  const draft = JSON.parse(await jsonEditor.inputValue()) as { streaming: string };
  draft.streaming = "Disabled";
  await jsonEditor.fill(JSON.stringify(draft, null, 2));

  const externalStatus = await page.evaluate(async () => {
    const configuration = (await fetch("/api/v1/protect/gateway-policies/configuration").then(
      (response) => response.json(),
    )) as {
      data: {
        revisionToken: string;
        settings: Array<{ id: string; key: string; family: string }>;
      };
    };
    const target = configuration.data.settings.find(
      (setting) => setting.family === "mcp" && setting.key === "mcpAuthorization",
    );
    if (!target) throw new Error("Missing mock MCP authorization policy");
    const response = await fetch(
      `/api/v1/protect/gateway-policies/${encodeURIComponent(target.id)}`,
      {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          revisionToken: configuration.data.revisionToken,
          value: { rules: [{ action: "allow", resource: "externally-updated" }] },
        }),
      },
    );
    return response.status;
  });
  expect(externalStatus).toBe(200);

  await page.waitForResponse(
    (response) =>
      response.request().method() === "GET" &&
      response.url().endsWith("/api/v1/protect/gateway-policies/configuration"),
    { timeout: 15_000 },
  );
  await dialog.getByRole("button", { name: "Save guardrails" }).click();
  await expect(dialog.getByText(/configuration changed/i)).toBeVisible();

  const streaming = await page.evaluate(async () => {
    const configuration = (await fetch("/api/v1/protect/gateway-policies/configuration").then(
      (response) => response.json(),
    )) as {
      data: {
        settings: Array<{ rawRef: { id: string }; value: { streaming?: string } }>;
      };
    };
    return configuration.data.settings.find(
      (setting) => setting.rawRef.id === "/llm/policies/guardrails",
    )?.value.streaming;
  });
  expect(streaming).toBe("Enabled");
});

test("Guardrails workspace and editor remain contained on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/protect/guardrails");
  await expect(page.getByRole("heading", { name: "LLM guardrails" })).toBeVisible();
  const overflow = await page
    .locator("body")
    .evaluate((body) => body.scrollWidth > body.clientWidth);
  expect(overflow).toBe(false);

  await page.getByRole("button", { name: "Edit guardrails" }).click();
  await expect(page.getByRole("dialog", { name: "Edit Guardrails" })).toBeVisible();
  const dialogOverflow = await page
    .locator(".dialog__body")
    .evaluate((body) => body.scrollWidth > body.clientWidth);
  expect(dialogOverflow).toBe(false);
});

test("Guardrail dialog traps keyboard focus and restores its trigger", async ({ page }) => {
  await page.goto("/protect/guardrails");
  const trigger = page.getByRole("button", { name: "Edit guardrails" });
  await trigger.focus();
  await trigger.click();

  const dialog = page.getByRole("dialog", { name: "Edit Guardrails" });
  await expect(dialog).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "Close dialog" })).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(dialog.getByRole("button", { name: "Save guardrails" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "Close dialog" })).toBeFocused();

  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(trigger).toBeFocused();
});
