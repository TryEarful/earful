import { expect, Page } from "@playwright/test";

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
