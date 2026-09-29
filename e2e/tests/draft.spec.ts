import AxeBuilder from "@axe-core/playwright";
import { test, expect } from "@playwright/test";
import { createPublishedSurvey, fakeMicrophone, minFillWait, offersVoice, scriptedVoice, submitTimeout } from "./helpers";

// Draft answers surviving a reload (SPEC.md story 79, M4-T8). Without a
// draft, a respondent who reloads part-way through loses every answer
// entered so far — most costly for long dictated answers, which are this
// product's primary answer type.
//
// The draft is held in the browser and nowhere else, so these tests
// assert both halves of that: the answers return after a reload, and
// nothing that must not be persisted is.

test("answers and position survive a reload, and clear on submit", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E draft ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);

  await respondent.locator("textarea").fill("Half an answer I do not want to retype.");
  await respondent.getByRole("button", { name: "Next" }).click();
  await respondent.getByLabel("Weekly").check();

  // The reload a respondent does by accident.
  await respondent.reload();

  await expect(respondent.locator("textarea")).toHaveValue(
    "Half an answer I do not want to retype."
  );
  await expect(respondent.getByLabel("Weekly")).toBeChecked();
  // And they come back where they were, not at the start.
  await expect(respondent.locator(".respond-progress")).toHaveText("Question 2 of 2");

  await minFillWait(respondent);
  await respondent.getByRole("button", { name: "Submit answers" }).click();
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible({
    timeout: submitTimeout,
  });

  // Submission is the point at which the draft has served its purpose. An
  // unsubmitted answer left in storage is readable by the next person to
  // use a shared device.
  const leftover = await respondent.evaluate(() =>
    Object.keys(window.localStorage).filter((k) => k.startsWith("earful.draft."))
  );
  expect(leftover).toEqual([]);

  await context.close();
});

test("the draft never holds a security field", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E draft fields ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);
  await respondent.locator("textarea").fill("Something worth saving.");

  const stored = await respondent.evaluate(() => {
    const key = Object.keys(window.localStorage).find((k) => k.startsWith("earful.draft."));
    return key ? window.localStorage.getItem(key) : null;
  });
  expect(stored).toBeTruthy();

  // Restoring any of these would either break the anti-abuse checks —
  // the render timestamp and proof-of-work belong to one page load — or
  // defeat them, in the honeypot's case.
  const parsed = JSON.parse(stored!);
  for (const forbidden of ["form_ts", "form_nonce", "altcha", "_csrf", "version_id"]) {
    expect(Object.keys(parsed.answers)).not.toContain(forbidden);
  }
  expect(stored).toContain("Something worth saving.");

  await context.close();
});

// Dictated answers are the case this feature matters most for, and the
// one most easily missed: assigning field.value from script fires no
// input event, so without an explicit dispatch the draft never sees a
// transcript.
test("a spoken answer is kept across a reload too", async ({ page, browser }) => {
  test.skip(
    !scriptedVoice,
    "refusing to send synthesized audio to a real transcriber (E2E_VOICE_MODE is not scripted)"
  );

  const share = await createPublishedSurvey(page, `E2E draft voice ${Date.now()}`);

  const context = await browser.newContext({
    storageState: undefined,
    permissions: ["microphone"],
  });
  await fakeMicrophone(context);
  const respondent = await context.newPage();
  await respondent.goto(share);

  const offered = await offersVoice(respondent);
  test.skip(!offered, "this instance has no transcription configured, so it offers no mic");

  await respondent.getByRole("button", { name: "Dictate" }).click();
  await respondent.getByRole("button", { name: "Use the microphone" }).click();
  await respondent.waitForTimeout(1500);
  await respondent.getByRole("button", { name: "Stop", exact: true }).click();

  // Wait for the stream to finish before reading: chunks arrive one after
  // another, and a value read mid-transcription is shorter than what the
  // draft ends up holding.
  await expect(respondent.locator(".voice-status").first()).toHaveText(/Transcribed/, {
    timeout: 15000,
  });
  const spoken = await respondent.locator("textarea").inputValue();
  expect(spoken.length).toBeGreaterThan(0);

  await respondent.reload();
  await expect(respondent.locator("textarea")).toHaveValue(spoken);

  await context.close();
});

// A republished survey may have reworded questions, so an answer typed
// against the old wording must not reappear under the new one.
test("a new version does not restore the old version's answers", async ({ page, browser }) => {
  const title = `E2E draft version ${Date.now()}`;
  const share = await createPublishedSurvey(page, title);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);
  await respondent.locator("textarea").fill("Answer to version one.");
  await respondent.waitForTimeout(150); // let the input handler store it

  // The creator publishes a second version. `page` is still on the
  // editor where createPublishedSurvey left it.
  const addForm = page.locator('form[action$="/questions"]');
  await addForm.locator('select[name="type"]').selectOption("short_text");
  await addForm.locator('input[name="text"]').fill("Anything else?");
  await addForm.getByRole("button", { name: "Add question" }).click();
  await page.getByRole("button", { name: /Publish version 2/ }).click();
  await expect(page.getByText("Published version 2")).toBeVisible();

  await respondent.reload();
  await expect(respondent.locator("textarea")).toHaveValue("");

  await context.close();
});

// Earlier versions of an answer (SPEC.md story 81). Dictation changes
// an answer in strokes too large for the browser's own undo, so the
// answer is remembered as it goes — in this browser, under the draft's
// rules — and an earlier state can be put back. The case that matters
// is the one asserted here: a Reset seconds after the last version must
// not take the text with it.
test("earlier versions of an answer are kept in the browser and can be restored", async ({
  page,
  browser,
}) => {
  const share = await createPublishedSurvey(page, `E2E versions ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);

  const offered = await offersVoice(respondent);
  test.skip(!offered, "versions are offered where dictation is, and this instance has none");

  const answer = respondent.locator("textarea");
  const link = respondent.getByRole("button", { name: "Previous versions" });
  await expect(link).toBeHidden(); // nothing to look at yet

  // One version per five seconds of editing, taken at the end of the
  // window.
  await answer.fill("First thought.");
  await expect(link).toBeVisible({ timeout: 7000 });

  // Then more, and a Reset before the next window has closed.
  await answer.fill("First thought. Second thought, which took a while to put into words.");
  await respondent.getByRole("button", { name: "Reset", exact: true }).click();
  await expect(answer).toHaveValue("");

  await link.click();
  const dialog = respondent.getByRole("dialog", { name: "Previous versions" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("never sent");
  const items = dialog.locator(".versions-item");
  await expect(items).toHaveCount(2);
  // Newest first, and the newest is what Reset destroyed.
  await expect(items.first()).toContainText("Second thought, which took a while");
  await expect(items.last().locator(".versions-text")).toHaveText("First thought.");
  await expect(items.first().locator("time")).toHaveText(/\d{1,2}:\d{2}:\d{2}/);

  const scan = await new AxeBuilder({ page: respondent }).include(".versions-dialog").analyze();
  expect(scan.violations).toEqual([]);

  await items.first().getByRole("button", { name: /Restore/ }).click();
  await expect(dialog).toBeHidden();
  await expect(answer).toHaveValue(
    "First thought. Second thought, which took a while to put into words."
  );

  // The history is the draft's kind of data: it survives a reload…
  await respondent.reload();
  await respondent.getByRole("button", { name: "Previous versions" }).click();
  await expect(respondent.locator(".versions-item")).toHaveCount(2);
  await respondent.keyboard.press("Escape");
  await expect(respondent.getByRole("dialog")).toBeHidden();

  // …and lives in this browser's storage, under the survey version.
  const stored = await respondent.evaluate(() =>
    Object.keys(localStorage).filter((name) => name.startsWith("earful.versions."))
  );
  expect(stored).toHaveLength(1);

  await context.close();
});
