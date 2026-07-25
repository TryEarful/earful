import { expect, BrowserContext, Page } from "@playwright/test";

const mailpitURL = process.env.E2E_MAILPIT_URL ?? "http://localhost:8025";

// Where magic-link emails can be read back from. "mailpit" (default) is
// the local compose inbox; "logging" is the staging smoke gate — there the
// app runs EMAIL_SENDER=console, so each email's lines land as individual
// Cloud Logging entries and the suite fetches them via the Logging API
// using the deploy workflow's short-lived WIF access token (needs
// E2E_LOG_PROJECT + E2E_GCP_ACCESS_TOKEN).
const linkSource = process.env.E2E_LINK_SOURCE ?? "mailpit";

// latestLinkTo polls the configured inbox for the newest message to addr
// and extracts the first URL matching pattern — the e2e equivalent of
// opening your inbox and clicking the link.
export async function latestLinkTo(addr: string, pattern: RegExp): Promise<string> {
  return linkSource === "logging"
    ? latestLinkViaLogging(addr, pattern)
    : latestLinkViaMailpit(addr, pattern);
}

async function latestLinkViaMailpit(addr: string, pattern: RegExp): Promise<string> {
  for (let attempt = 0; attempt < 20; attempt++) {
    const search = await fetch(
      `${mailpitURL}/api/v1/search?query=${encodeURIComponent("to:" + addr)}`
    );
    const results = (await search.json()) as { messages: { ID: string }[] };
    if (results.messages?.length > 0) {
      const message = await fetch(
        `${mailpitURL}/api/v1/message/${results.messages[0].ID}`
      );
      const body = (await message.json()) as { Text: string };
      const match = body.Text.match(pattern);
      if (match) return match[0];
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`no email with a matching link arrived for ${addr}`);
}

type LogEntry = { textPayload?: string; timestamp: string };

async function latestLinkViaLogging(addr: string, pattern: RegExp): Promise<string> {
  const project = process.env.E2E_LOG_PROJECT;
  const token = process.env.E2E_GCP_ACCESS_TOKEN;
  if (!project || !token) {
    throw new Error("E2E_LINK_SOURCE=logging needs E2E_LOG_PROJECT and E2E_GCP_ACCESS_TOKEN");
  }
  const windowStart = new Date(Date.now() - 2 * 60 * 1000).toISOString();
  // Logging ingest lags a few seconds (vs mailpit's near-instant API), so
  // poll slower for longer.
  for (let attempt = 0; attempt < 24; attempt++) {
    const resp = await fetch("https://logging.googleapis.com/v2/entries:list", {
      method: "POST",
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: JSON.stringify({
        resourceNames: [`projects/${project}`],
        filter: `resource.type="cloud_run_revision" AND textPayload:* AND timestamp>="${windowStart}"`,
        orderBy: "timestamp desc",
        pageSize: 500,
      }),
    });
    if (resp.ok) {
      const data = (await resp.json()) as { entries?: LogEntry[] };
      const entries = data.entries ?? [];
      // The console sender writes the whole email in one stdout write;
      // Cloud Run splits it into one entry per line with near-identical
      // timestamps. Find the newest "To: <addr>" line, then the link line
      // from the same email (within 2s of it).
      const toIdx = entries.findIndex((e) => e.textPayload?.trim() === `To: ${addr}`);
      if (toIdx !== -1) {
        const toTime = Date.parse(entries[toIdx].timestamp);
        for (const e of entries) {
          if (Math.abs(Date.parse(e.timestamp) - toTime) > 2000) continue;
          const match = e.textPayload?.match(pattern);
          if (match) return match[0];
        }
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 5000));
  }
  throw new Error(`no logged email with a matching link found for ${addr}`);
}

// fakeMicrophone gives a context a synthetic capture device: a 440 Hz
// tone from the page's own AudioContext, handed back as a MediaStream.
//
// This replaced Chromium's --use-fake-device-for-media-capture, which is
// a no-op in Chrome 149: enumerateDevices shows only real hardware with
// the flag set, so the suite was quietly recording from the developer's
// actual microphone on a laptop, and failing with NotFoundError on CI
// runners, which have no audio device at all. A stream built in the page
// needs no hardware, behaves the same on every machine, and stops the
// test suite opening anybody's mic.
//
// Everything downstream of getUserMedia — the PCM conversion, the
// socket, the transcript, the caps — is the code under test and is
// untouched. What is skipped is the browser's own device plumbing, which
// is not ours to test.
export async function fakeMicrophone(context: BrowserContext): Promise<void> {
  await context.addInitScript(() => {
    const audio = new AudioContext();
    const tone = audio.createOscillator();
    const sink = audio.createMediaStreamDestination();
    tone.frequency.value = 440;
    tone.connect(sink);
    tone.start();
    navigator.mediaDevices.getUserMedia = async () => {
      // An AudioContext created without a user gesture starts suspended,
      // and a suspended graph produces nothing. This call always happens
      // inside the consent click, which is a gesture.
      await audio.resume();
      return sink.stream;
    };
  });
}

// --- What this instance offers -------------------------------------
//
// "An absent capability is an absent feature" (SPEC.md Appendix D): an
// instance with no AI configured renders no mic, no drafting panel and
// no insight card, and that is correct behaviour rather than a fault.
// One suite has to gate a laptop running the scripted provider, a CI
// compose stack, and staging running Vertex — so it asks the page what
// is on offer and asserts accordingly, instead of assuming.
//
// The probes read server-rendered markers, not the JavaScript-built UI,
// so they answer the same way with scripting disabled.

// offersAIDrafting: the M6-T3 panel on a survey editor page.
export async function offersAIDrafting(page: Page): Promise<boolean> {
  return (await page.locator("#ai-generate").count()) > 0;
}

// offersVoice: the M5 socket endpoint on a respondent page. voice.js
// builds the mic from this attribute; without it there is no mic.
export async function offersVoice(page: Page): Promise<boolean> {
  return (await page.locator("form[data-voice-path]").count()) > 0;
}

// offersInsights: the M10 panel on a results page. It also needs at
// least one response, so probe after answers exist.
export async function offersInsights(page: Page): Promise<boolean> {
  return (await page.locator("#insights").count()) > 0;
}

// E2E_AI_MODE says what sits behind the AI seam: "scripted" (canned,
// deterministic output — the compose stack and CI) or "real" (an actual
// model). Assertions about generated *content* only hold for scripted;
// against a real model the suite asserts the behaviour around the
// content instead. Chromium's fake capture device is the reason this
// distinction matters most: it emits a tone, so a real transcriber
// correctly hears no words where the scripted one returns a sentence.
export const scriptedAI = (process.env.E2E_AI_MODE ?? "scripted") === "scripted";

// E2E_VOICE_MODE says the same thing about transcription specifically,
// because the two need not match: staging runs text AI on Vertex and
// transcription on the scripted provider, so that a suite with no
// microphone is not feeding synthesized audio to a real speech model in
// a loop. Defaults to whatever E2E_AI_MODE says.
export const scriptedVoice =
  (process.env.E2E_VOICE_MODE ?? process.env.E2E_AI_MODE ?? "scripted") === "scripted";

// aiTimeout: canned output returns immediately; a real model is allowed
// to think.
export const aiTimeout = scriptedAI ? 15000 : 60000;

export function uniqueEmail(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.test`;
}

// signIn drives the full magic-link flow for a fresh account.
export async function signIn(page: Page, addr: string): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email address").fill(addr);
  await page.getByRole("button", { name: "Email me a sign-in link" }).click();
  await expect(page.getByRole("heading", { name: "Check your email" })).toBeVisible();

  const link = await latestLinkTo(addr, /https?:\/\/[^\s]+\/auth\/magic\/verify\?token=[\w-]+/);
  await page.goto(link);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
}

// createPublishedSurvey builds a two-question survey through the UI and
// returns its share path.
export async function createPublishedSurvey(page: Page, title: string): Promise<string> {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(title);
  await page.getByRole("button", { name: "Create survey" }).click();
  await expect(page.getByRole("heading", { name: title })).toBeVisible();

  const addForm = page.locator('form[action$="/questions"]');
  await addForm.locator('select[name="type"]').selectOption("long_text");
  await addForm.locator('input[name="text"]').fill("What would make surveys less painful?");
  await addForm.getByRole("button", { name: "Add question" }).click();

  await addForm.locator('select[name="type"]').selectOption("single_choice");
  await addForm.locator('input[name="text"]').fill("How often do you answer surveys?");
  await addForm.locator('textarea[name="options"]').fill("Weekly\nMonthly\nAlmost never");
  await addForm.getByRole("button", { name: "Add question" }).click();

  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();

  const share = await page.locator(".share-link a").getAttribute("href");
  if (!share) throw new Error("no share link after publishing");
  return share;
}

// minFillWait outlasts the server's minimum-fill-time bot check (3s).
// The margin is generous on purpose: with only 500ms of slack, a VM
// clock resync or a busy host made the check flake — and this suite
// gates staging promotion, where a flake blocks deploys.
export async function minFillWait(page: Page): Promise<void> {
  await page.waitForTimeout(5000);
}
