import { defineConfig, devices } from "@playwright/test";

// The stack under test is brought up separately (`make dev-up` locally,
// `make test-e2e` on CI); this config never starts a server of its own.
// The browser runs on the host, which is also where the compose stack
// publishes its ports, so the UI behaves exactly as it does for a developer.
export default defineConfig({
  testDir: "./e2e",
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [["list"], ["github"]] : "list",
  timeout: 60_000,
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000",
    navigationTimeout: 60_000,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
