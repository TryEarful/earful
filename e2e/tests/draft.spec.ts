import { test, expect } from "@playwright/test";
import { createPublishedSurvey, fakeMicrophone, minFillWait, offersVoice, scriptedVoice } from "./helpers";

// Draft answers surviving a reload (SPEC.md story 79, M4-T8). Found by
// dogfooding the live product: a respondent part-way through who
// refreshes loses everything, which is worst for exactly the long spoken
// answers this product exists to collect.
//
// The draft lives in the browser and nowhere else, so these tests assert
// both halves of that: the answers come back, and nothing that must not
// be stored is stored.

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
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible();

  // Submitted is the moment the draft has done its job. An unsent answer
  // left behind is just somebody's words sitting on a borrowed computer.
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

// The answer this feature exists for. Setting field.value from script
// fires no input event, so a transcript would have been the one kind of
// answer the draft silently missed — and it is the most expensive to
// lose and the last anyone wants to repeat.
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

  await respondent.getByRole("button", { name: "Answer by speaking" }).click();
  await respondent.getByRole("button", { name: "Use the microphone" }).click();
  await respondent.waitForTimeout(1500);
  await respondent.getByRole("button", { name: "Stop and transcribe" }).click();

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
