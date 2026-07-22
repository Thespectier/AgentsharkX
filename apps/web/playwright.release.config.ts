import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  testMatch: "release-real.spec.ts",
  timeout: 45_000,
  fullyParallel: false,
  workers: 1,
  reporter: "list",
  use: {
    ...devices["Desktop Chrome"],
    baseURL: process.env.AGENTSHARK_RELEASE_BASE_URL ?? "http://127.0.0.1:5173",
    colorScheme: "dark",
    trace: "retain-on-failure",
  },
});
