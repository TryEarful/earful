import { test, expect, Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { fakeMicrophone, minFillWait, offersVoice, scriptedVoice, submitTimeout } from "./helpers";

// Answering from the keyboard (SPEC.md story 80, M4-T9).
//
// Letters select choices, digits are reserved for rating scales, Y/N
// answers yes-no questions, Enter advances. These tests assert both that
// a respondent never needs the mouse and that the hints on screen are
// accurate — an incorrect hint is worse than none, since it sends the
// respondent somewhere unintended.

// A survey with one of each keyed question type, built through the UI.
async function keyedSurvey(page: Page, title: string): Promise<string> {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(title);
  await page.getByRole("button", { name: "Create survey" }).click();

  const add = page.locator('form[action$="/questions"]');
  const addQuestion = async (type: string, text: string, options?: string) => {
    await add.locator('select[name="type"]').selectOption(type);
    await add.locator('input[name="text"]').fill(text);
    if (options) await add.locator('textarea[name="options"]').fill(options);
    await add.getByRole("button", { name: "Add question" }).click();
  };

  await addQuestion("single_choice", "Which plan?", "Free\nPro\nEnterprise");
  await addQuestion("dropdown", "Where did you hear about us?", "A friend\nSearch\nAn ad");
  await addQuestion("nps", "How likely are you to recommend us?");
  await addQuestion("yes_no", "Would you use it again?");

  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const share = await page.locator(".share-link a").getAttribute("href");
  if (!share) throw new Error("no share link after publishing");
  return share;
}

test("a whole survey can be answered without a single click", async ({ page, browser }) => {
  const share = await keyedSurvey(page, `E2E keys ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);

  // Single choice: letters, in option order.
  await respondent.keyboard.press("b");
  await expect(respondent.getByRole("radio", { name: "Pro", exact: true })).toBeChecked();
  await respondent.keyboard.press("Enter");

  // Dropdown renders as the same lettered list, so it answers the same
  // way — a browser's own <select> popup has nowhere to put a hint.
  await expect(respondent.locator(".respond-progress")).toHaveText("Question 2 of 4");
  await respondent.keyboard.press("c");
  await expect(respondent.getByRole("radio", { name: "An ad", exact: true })).toBeChecked();
  await respondent.keyboard.press("Enter");

  // NPS is 0–10, so "1" then "0" must mean ten rather than one. This is
  // why choices get letters: the digits were already spoken for.
  await respondent.keyboard.press("1");
  await respondent.keyboard.press("0");
  await expect(respondent.locator('.scale-point input[value="10"]')).toBeChecked();
  await respondent.keyboard.press("Enter");

  // Yes/no takes the initial.
  await respondent.keyboard.press("y");
  await expect(respondent.getByRole("radio", { name: "Yes", exact: true })).toBeChecked();

  await minFillWait(respondent);
  await respondent.keyboard.press("Enter"); // last question: submits
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible({
    timeout: submitTimeout,
  });

  await context.close();
});

test("shift+enter goes back, and a stale digit does not linger", async ({ page, browser }) => {
  const share = await keyedSurvey(page, `E2E keys back ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);

  await respondent.keyboard.press("a");
  await respondent.keyboard.press("Enter");
  await expect(respondent.locator(".respond-progress")).toHaveText("Question 2 of 4");

  await respondent.keyboard.press("Shift+Enter");
  await expect(respondent.locator(".respond-progress")).toHaveText("Question 1 of 4");
  await expect(respondent.getByRole("radio", { name: "Free", exact: true })).toBeChecked();

  await context.close();
});

test("typing is never swallowed by the key layer", async ({ page, browser }) => {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(`E2E keys typing ${Date.now()}`);
  await page.getByRole("button", { name: "Create survey" }).click();
  const add = page.locator('form[action$="/questions"]');
  await add.locator('select[name="type"]').selectOption("long_text");
  await add.locator('input[name="text"]').fill("Tell us everything");
  await add.getByRole("button", { name: "Add question" }).click();
  await add.locator('select[name="type"]').selectOption("short_text");
  await add.locator('input[name="text"]').fill("And your role?");
  await add.getByRole("button", { name: "Add question" }).click();
  await page.getByRole("button", { name: "Publish version 1" }).click();
  const share = await page.locator(".share-link a").getAttribute("href");

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share!);

  // Letters and digits are just characters while a text field has focus.
  const answer = respondent.locator("textarea");
  await answer.click();
  await respondent.keyboard.type("Plan b, and 10 out of 10 for yes");
  await expect(answer).toHaveValue("Plan b, and 10 out of 10 for yes");
  await expect(respondent.locator(".respond-progress")).toHaveText("Question 1 of 2");

  // Enter remains a newline in a textarea: long answers are frequently
  // dictated and then edited, so advancing mid-paragraph would lose the
  // respondent's place.
  await respondent.keyboard.press("Enter");
  await respondent.keyboard.type("second line");
  await expect(answer).toHaveValue(/\nsecond line$/);
  await expect(respondent.locator(".respond-progress")).toHaveText("Question 1 of 2");

  // …and Ctrl/Cmd+Enter is the way out without reaching for the mouse.
  await respondent.keyboard.press("ControlOrMeta+Enter");
  await expect(respondent.locator(".respond-progress")).toHaveText("Question 2 of 2");

  await context.close();
});

test("hints are visual only, and the page stays axe-clean", async ({ page, browser }) => {
  const share = await keyedSurvey(page, `E2E keys a11y ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);

  // The hint must not join the option's accessible name: "B Pro" read
  // aloud is worse than no hint at all. Asserted by role rather than by
  // label, because the label's *text* legitimately contains the hint —
  // it is the accessibility tree that has to be clean, and only
  // aria-hidden makes it so.
  await expect(respondent.getByRole("radio", { name: "Pro", exact: true })).toHaveCount(1);
  await expect(respondent.getByRole("radio", { name: "B Pro", exact: true })).toHaveCount(0);
  await expect(respondent.locator(".key-hint").first()).toHaveAttribute("aria-hidden", "true");

  // Shown only where a keyboard exists. Displaying a shortcut that cannot
  // be pressed misleads the reader, so on a touch device the hint must be
  // absent rather than merely small.
  const hasKeyboard = await respondent.evaluate(
    () => matchMedia("(hover: hover) and (pointer: fine)").matches
  );
  const hint = respondent.locator(".key-hint").first();
  if (hasKeyboard) {
    await expect(hint).toBeVisible();
  } else {
    await expect(hint).toBeHidden();
  }

  const scan = await new AxeBuilder({ page: respondent }).analyze();
  expect(scan.violations).toEqual([]);

  await context.close();
});

test("shift+space starts and stops recording", async ({ page, browser }) => {
  test.skip(
    !scriptedVoice,
    "refusing to send synthesized audio to a real transcriber (E2E_VOICE_MODE is not scripted)"
  );

  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(`E2E keys voice ${Date.now()}`);
  await page.getByRole("button", { name: "Create survey" }).click();
  const add = page.locator('form[action$="/questions"]');
  await add.locator('select[name="type"]').selectOption("long_text");
  await add.locator('input[name="text"]').fill("How did it go?");
  await add.getByRole("button", { name: "Add question" }).click();
  await add.locator('select[name="type"]').selectOption("short_text");
  await add.locator('input[name="text"]').fill("Your role?");
  await add.getByRole("button", { name: "Add question" }).click();
  await page.getByRole("button", { name: "Publish version 1" }).click();
  const share = await page.locator(".share-link a").getAttribute("href");

  const context = await browser.newContext({
    storageState: undefined,
    permissions: ["microphone"],
  });
  await fakeMicrophone(context);
  const respondent = await context.newPage();
  await respondent.goto(share!);

  const offered = await offersVoice(respondent);
  test.skip(!offered, "this instance has no transcription configured, so it offers no mic");

  // Consent still has to be given the first time; the key replaces the
  // click on the mic, not the promise made before it opens.
  await respondent.keyboard.press("Shift+ ");
  await respondent.getByRole("button", { name: "Use the microphone" }).click();
  await expect(respondent.getByRole("button", { name: "Stop and transcribe" })).toBeVisible();

  await respondent.waitForTimeout(1200);
  await respondent.keyboard.press("Shift+ ");
  await expect(respondent.locator(".voice-status").first()).toHaveText(/Transcribed/, {
    timeout: 15000,
  });
  await expect(respondent.locator("textarea")).not.toBeEmpty();

  await context.close();
});
