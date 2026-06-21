import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./visual-contract/tests",
  testMatch: /browser-evidence-collector\.spec\.mjs/,
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? "github" : "list",
  use: {
    trace: "off",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
