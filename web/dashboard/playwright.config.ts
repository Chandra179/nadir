import { defineConfig, devices } from "@playwright/test";

const localBrowser = process.env.PLAYWRIGHT_EXECUTABLE_PATH ?? (process.env.CI ? undefined : "/usr/bin/google-chrome");
const baseURL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const port = new URL(baseURL).port || "3000";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: "list",
  timeout: process.env.E2E_LIVE === "1" ? 180_000 : 30_000,
  use: {
    baseURL,
    trace: "on-first-retry",
    ...(localBrowser ? { launchOptions: { executablePath: localBrowser } } : {}),
  },
  webServer: {
    command: `npm run dev -- --host 127.0.0.1 --port ${port} --configLoader runner`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
