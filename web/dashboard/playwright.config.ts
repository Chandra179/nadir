import { defineConfig, devices } from "@playwright/test";

const localBrowser = process.env.PLAYWRIGHT_EXECUTABLE_PATH ?? (process.env.CI ? undefined : "/usr/bin/google-chrome");

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000",
    trace: "on-first-retry",
    ...(localBrowser ? { launchOptions: { executablePath: localBrowser } } : {}),
  },
  webServer: {
    command: "npm run dev -- --host 127.0.0.1 --configLoader runner",
    url: "http://127.0.0.1:3000",
    reuseExistingServer: !process.env.CI,
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
