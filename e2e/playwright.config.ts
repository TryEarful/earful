import { defineConfig, devices } from "@playwright/test";

// The suite drives the real compose stack: app on :8080, mailpit (the
// local inbox) on :8025. `make e2e-smoke` brings both up first.
//
// A setup project signs in once and saves storage state; the three width
// projects — phone, tablet, desktop, per M4-T1's acceptance criteria —
// reuse it, so the suite stays inside the app's own magic-link rate
// limits.
// Staging sits behind HTTP Basic Auth; E2E_BASIC_AUTH ("user:pass") is
// unset for local runs.
//
// A static Authorization header, not httpCredentials: Playwright's auth
// interception stalls every navigation against Cloud Run's HTTP/2 front
// end — measured again while chasing the WebSocket 401s below, in both
// challenge-response and send:"always" modes, so this is not a stale
// note. Applying it to every host is deliberate: the suite hits both the
// run.app URL (E2E_BASE_URL) and the custom domain magic links point at.
//
// The header only covers requests the page makes. The WebSocket
// handshake and the audio worklet are fetched by the browser outside
// that context and arrive without it — which is why the wall now hands
// out a cookie on the first authenticated request and accepts it
// afterwards (internal/http/basicauth.go). That fix is for people as
// much as for this suite: Chrome does not send cached credentials on a
// handshake either, so voice on staging was broken for anyone.
const basicAuth = process.env.E2E_BASIC_AUTH ?? "";
const extraHTTPHeaders = basicAuth.includes(":")
  ? { Authorization: "Basic " + Buffer.from(basicAuth).toString("base64") }
  : undefined;

export default defineConfig({
  testDir: "./tests",
  fullyParallel: false, // sequential keeps per-IP rate limits far away
  workers: 1,
  retries: 0,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:8080",
    trace: "retain-on-failure",
    extraHTTPHeaders,
    // No --use-fake-device-for-media-capture here: it is a no-op in
    // Chrome 149 (enumerateDevices returns only real hardware with the
    // flag set), which meant the voice tests were recording from the
    // developer's actual microphone locally and failing on CI runners,
    // which have no audio device at all. The mic is synthesized in the
    // page instead — see fakeMicrophone in tests/helpers.ts.
    launchOptions: {
      // Still useful: it auto-accepts the permission prompt, so a voice
      // test that ever reaches the real getUserMedia does not hang.
      args: ["--use-fake-ui-for-media-stream"],
    },
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
