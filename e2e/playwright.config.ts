import { defineConfig, devices } from "@playwright/test";

// The suite drives the real compose stack: app on :8080, mailpit (the
// local inbox) on :8025. `make e2e-smoke` brings both up first.
//
// A setup project signs in once and saves storage state; the three width
// projects — phone, tablet, desktop, per M4-T1's acceptance criteria —
// reuse it, so the suite stays inside the app's own magic-link rate
// limits.
export default defineConfig({
  testDir: "./tests",
  fullyParallel: false, // sequential keeps per-IP rate limits far away
  workers: 1,
  retries: 0,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:8080",
    trace: "retain-on-failure",
  },
  projects: [
    { name: "setup", testMatch: /auth\.setup\.ts/ },
    {
      name: "desktop",
      dependencies: ["setup"],
      use: { ...devices["Desktop Chrome"], storageState: ".auth/creator.json" },
    },
    {
      name: "tablet",
      dependencies: ["setup"],
      use: { ...devices["iPad Mini"], defaultBrowserType: "chromium", storageState: ".auth/creator.json" },
    },
    {
      name: "phone",
      dependencies: ["setup"],
      use: { ...devices["Pixel 7"], storageState: ".auth/creator.json" },
    },
  ],
});
