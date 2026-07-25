import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createPublishedSurvey, minFillWait } from "./helpers";

// All tests run signed in as the shared creator (see auth.setup.ts);
// respondents get their own fresh, unauthenticated contexts.

// The core-journey smoke test (SPEC.md story 72's local form): create →
// share → answer → results, through the real UI with real JavaScript —
// the paged respondent flow, the invisible ALTCHA solve, all of it. Runs
// at phone, tablet and desktop widths (M4-T1 AC).
test("core loop: build, publish, answer, count", async ({ page, browser }) => {
  const title = `E2E smoke ${Date.now()}`;
  const share = await createPublishedSurvey(page, title);

  // A respondent is a different person: fresh context, no session.
  const respondentContext = await browser.newContext({ storageState: undefined });
  const respondent = await respondentContext.newPage();
  await respondent.goto(share);
  await expect(respondent.getByRole("heading", { name: title })).toBeVisible();

  // The enhancement paginates: question 1 visible, question 2 hidden.
  await expect(respondent.getByText("What would make surveys less painful?")).toBeVisible();
  await expect(respondent.getByText("How often do you answer surveys?")).toBeHidden();

  // Navigation offers only the moves that exist. Both of these were
  // broken in production and neither was caught here, because the suite
  // only ever asked whether the buttons *worked*: `button { display:
  // inline-block }` outranks the browser's [hidden] rule, so setting
  // .hidden on a button did nothing at all. Pressing Next on the last
  // question then looked like a dead button.
  await expect(respondent.getByRole("button", { name: "Back" })).toBeHidden();
  await expect(respondent.getByRole("button", { name: "Next" })).toBeVisible();

  await respondent.locator("textarea").fill("Let me talk instead of type.");
  await respondent.getByRole("button", { name: "Next" }).click();
  await respondent.getByLabel("Monthly").check();

  // Last question: Next is gone, Back and Submit are the way on.
  await expect(respondent.getByRole("button", { name: "Next" })).toBeHidden();
  await expect(respondent.getByRole("button", { name: "Back" })).toBeVisible();
  await expect(respondent.getByRole("button", { name: "Submit answers" })).toBeVisible();

  await minFillWait(respondent);
  await respondent.getByRole("button", { name: "Submit answers" }).click();
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible();
  await respondentContext.close();

  // The creator sees the response counted.
  await page.reload();
  await expect(page.getByText("1 response")).toBeVisible();
});

// The no-JS contract (story 29): the same form, JavaScript disabled,
// still renders every question and submits.
test("respondent form works with JavaScript disabled", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E no-JS ${Date.now()}`);

  const noJS = await browser.newContext({ storageState: undefined, javaScriptEnabled: false });
  const respondent = await noJS.newPage();
  await respondent.goto(share);

  // Both questions visible at once — the whole form, no paging.
  await expect(respondent.getByText("What would make surveys less painful?")).toBeVisible();
  await expect(respondent.getByText("How often do you answer surveys?")).toBeVisible();

  await respondent.locator("textarea").fill("Typed without any JavaScript.");
  await respondent.getByLabel("Weekly").check();
  await minFillWait(respondent);
  await respondent.getByRole("button", { name: "Submit answers" }).click();
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible();
  await noJS.close();
});

// Accessibility (M4-T1 AC: axe-core clean on respondent pages; login and
// dashboard held to the same bar).
test("respondent page is axe-clean", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E axe ${Date.now()}`);

  const respondentContext = await browser.newContext({ storageState: undefined });
  const respondent = await respondentContext.newPage();
  await respondent.goto(share);
  await expect(respondent.locator(".respond-form")).toBeVisible();

  const results = await new AxeBuilder({ page: respondent }).analyze();
  expect(results.violations).toEqual([]);
  await respondentContext.close();
});

test("login page is axe-clean", async ({ browser }) => {
  // Fresh unauthenticated context: with a session, /login redirects to
  // the dashboard and the scan would silently test the wrong page.
  const anonymous = await browser.newContext({ storageState: undefined });
  const page = await anonymous.newPage();
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
  await anonymous.close();
});

test("dashboard is axe-clean", async ({ page }) => {
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
});

// Responsive floor: no horizontal scrolling at any project width, on
// creator and respondent pages alike (story 18).
test("no horizontal overflow", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E overflow ${Date.now()}`);

  const overflows = async (p: typeof page) =>
    p.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);

  for (const path of ["/dashboard", "/account"]) {
    await page.goto(path);
    expect(await overflows(page), `${path} overflows horizontally`).toBe(false);
  }

  const respondentContext = await browser.newContext({ storageState: undefined });
  const respondent = await respondentContext.newPage();
  await respondent.goto(share);
  expect(await overflows(respondent), "respondent page overflows horizontally").toBe(false);
  await respondentContext.close();
});
