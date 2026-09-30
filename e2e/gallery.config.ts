import { defineConfig } from "@playwright/test";

// The design gallery (gallery/gallery.spec.ts) is not part of the suite:
// it takes pictures for people to look at and asserts nothing about the
// product. `make gallery` runs it against the compose stack.
export default defineConfig({
  testDir: "./gallery",
  workers: 1,
  retries: 0,
  reporter: "line",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:8080",
    locale: "en-US",
  },
});
