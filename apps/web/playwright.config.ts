import { defineConfig, devices } from "@playwright/test";

const previewPort = Number(process.env.AGENTSHARK_PLAYWRIGHT_PORT ?? "4173");

export default defineConfig({
  testDir: "./tests",
  snapshotPathTemplate: "../../docs/screenshots/{arg}{ext}",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: [["list"], ["html", { open: "never" }]],
  expect: {
    toHaveScreenshot: {
      animations: "disabled",
      caret: "hide",
      maxDiffPixelRatio: 0.001,
    },
  },
  use: {
    baseURL: `http://127.0.0.1:${previewPort}`,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    colorScheme: "dark",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: `npm run build && npm run preview -- --port ${previewPort}`,
    env: { VITE_ENABLE_MOCKS: "true" },
    url: `http://127.0.0.1:${previewPort}`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
