import { test, expect } from "@playwright/test";
import { aiTimeout, offersAIDrafting, offersSurveyFromDescription, scriptedAI } from "./helpers";

// AI-drafted questions in a real browser (M6-T3). The compose stack runs
// the scripted provider, which emits the same NDJSON shape the prompt
// asks a model for — so this exercises the socket, the streaming output
// and the parse, without a model.
//
// Against an instance with no AI configured there is no panel to drive,
// and each test says so rather than failing: the feature is absent by
// configuration, and what has to keep working is the editor around it.

test("questions stream in and land in the draft", async ({ page }) => {
  // Canned output returns instantly; a real model has to think, and the
  // default 30s budget covers neither the wait nor the page around it.
  if (!scriptedAI) test.slow();

  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(`E2E generate ${Date.now()}`);
  await page.getByRole("button", { name: "Create survey" }).click();

  const offered = await offersAIDrafting(page);
  if (!offered) {
    // No drafting panel, but still a survey editor: questions are added
    // by hand and the survey publishes.
    await expect(page.locator('form[action$="/questions"]')).toBeVisible();
  }
  test.skip(!offered, "this instance has no AI configured, so it offers no drafting panel");

  const panel = page.locator("#ai-generate");
  await expect(panel).toBeVisible();

  // This test is about the streamed path, so wait until generate.js says
  // it owns the submit. Clicking earlier is not a failure — the plain
  // POST still drafts the questions — but it is the other test.
  await expect(panel.locator(".js-generate-form[data-enhanced]")).toBeAttached();

  await panel.locator('textarea[name="prompt"]').fill("how our first week feels to a new customer");
  await panel.getByRole("button", { name: "Draft questions" }).click();

  // Output appears while the model is still talking, ends with the
  // summary, and the editor then shows the questions themselves. The
  // first fragment is where a real model's thinking time lands, so this
  // gets the AI budget rather than the default 5s.
  await expect(panel.locator(".js-generate-output")).not.toBeEmpty({ timeout: aiTimeout });
  await expect(panel.locator(".js-generate-output")).toContainText(/Added \d+ questions?/, {
    timeout: aiTimeout,
  });

  // They are ordinary draft questions: listed, and editable.
  const questions = page.locator(".js-questions .js-question");
  await expect(questions.first()).toBeVisible({ timeout: aiTimeout });
  expect(await questions.count()).toBeGreaterThan(2);
  await expect(page.getByRole("button", { name: "Publish version 1" })).toBeVisible();
});

// The same feature with JavaScript off: slower, no live output, same
// result. This is the contract that keeps the socket an enhancement.
test("drafting with AI works without JavaScript", async ({ browser }) => {
  const context = await browser.newContext({
    storageState: ".auth/creator.json",
    javaScriptEnabled: false,
  });
  const page = await context.newPage();

  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(`E2E generate no-JS ${Date.now()}`);
  await page.getByRole("button", { name: "Create survey" }).click();

  const offered = await offersAIDrafting(page);
  if (!offered) {
    await expect(page.locator('form[action$="/questions"]')).toBeVisible();
    await context.close();
  }
  test.skip(!offered, "this instance has no AI configured, so it offers no drafting panel");

  const panel = page.locator("#ai-generate");
  await panel.locator('textarea[name="prompt"]').fill("what to ask after a support call");
  await panel.getByRole("button", { name: "Draft questions" }).click();

  await expect(page.getByText(/Added \d+ questions? to your draft/)).toBeVisible({
    timeout: aiTimeout + 5000,
  });
  expect(await page.locator(".js-questions .js-question").count()).toBeGreaterThan(2);

  await context.close();
});

// A survey started from a description (issue #20). The form is a plain
// POST that waits for the model, so it is driven with JavaScript on and
// off; the scripted provider answers with a title line and questions.
for (const javaScriptEnabled of [true, false]) {
  test(`a survey starts from a description${javaScriptEnabled ? "" : " without JavaScript"}`, async ({ browser }) => {
    if (!scriptedAI) test.slow();
    const context = await browser.newContext({ storageState: ".auth/creator.json", javaScriptEnabled });
    const page = await context.newPage();

    await page.goto("/surveys/new");
    const offered = await offersSurveyFromDescription(page);
    if (!offered) {
      // No description, but the form still creates a survey by hand.
      await expect(page.getByLabel("Title")).toBeVisible();
      await context.close();
    }
    test.skip(!offered, "this instance has no AI configured, so it offers no description");

    // The title is left empty: the model proposes it.
    const topic = `what to ask after a support call ${Date.now()}`;
    await page.locator(".js-new-survey-prompt").fill(topic);
    await page.getByRole("button", { name: "Create survey" }).click();

    await expect(page).toHaveURL(/\/surveys\/[0-9a-f-]+\?added=\d+/, { timeout: aiTimeout + 5000 });
    await expect(page.getByText(/Added \d+ questions? to your draft/)).toBeVisible();
    if (scriptedAI) {
      await expect(page.locator("h1")).toHaveText(`Survey about ${topic}`);
    } else {
      await expect(page.locator("h1")).not.toBeEmpty();
    }

    // They are ordinary draft questions: listed, and ready to publish.
    expect(await page.locator(".js-questions .js-question").count()).toBeGreaterThan(2);
    await expect(page.getByRole("button", { name: "Publish version 1" })).toBeVisible();

    await context.close();
  });
}

// Files attached to the panel (issue #5). They go with the plain post,
// with JavaScript on or off: the socket carries text only. The scripted
// provider names what it was sent in one more question, which is how a
// browser can see the files reached the model.
for (const javaScriptEnabled of [true, false]) {
  test(`attached files reach the model${javaScriptEnabled ? "" : " without JavaScript"}`, async ({ browser }) => {
    if (!scriptedAI) test.slow();
    const context = await browser.newContext({ storageState: ".auth/creator.json", javaScriptEnabled });
    const page = await context.newPage();

    await page.goto("/surveys/new");
    await page.getByLabel("Title").fill(`E2E attach ${Date.now()}`);
    await page.getByRole("button", { name: "Create survey" }).click();

    const offered = await offersAIDrafting(page);
    if (!offered) {
      await expect(page.locator('form[action$="/questions"]')).toBeVisible();
      await context.close();
    }
    test.skip(!offered, "this instance has no AI configured, so it offers no drafting panel");

    const panel = page.locator("#ai-generate");
    if (javaScriptEnabled) await expect(panel.locator(".js-generate-form[data-enhanced]")).toBeAttached();
    await panel.locator('textarea[name="prompt"]').fill("use the attached notes and answers");
    await panel.locator(".js-attach-files").setInputFiles([
      { name: "notes.md", mimeType: "text/markdown", buffer: Buffer.from("# Onboarding\n\n- What helped in week one?\n") },
      { name: "answers.csv", mimeType: "text/csv", buffer: Buffer.from("question,answer\nHow did you find us?,A friend\n") },
    ]);
    await panel.getByRole("button", { name: "Draft questions" }).click();

    await expect(page.getByText(/Added \d+ questions? to your draft/)).toBeVisible({ timeout: aiTimeout + 5000 });
    const questions = page.locator(".js-questions .js-question");
    expect(await questions.count()).toBeGreaterThan(2);
    if (scriptedAI) {
      await expect(questions.filter({ hasText: "notes.md, answers.csv" })).toHaveCount(1);
    }

    await context.close();
  });
}

// A file the panel cannot use comes back with the prompt as typed and a
// message naming the file.
test("a file that cannot be used is refused by name", async ({ page }) => {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(`E2E attach refused ${Date.now()}`);
  await page.getByRole("button", { name: "Create survey" }).click();
  const offered = await offersAIDrafting(page);
  test.skip(!offered, "this instance has no AI configured, so it offers no drafting panel");

  const panel = page.locator("#ai-generate");
  await panel.locator('textarea[name="prompt"]').fill("about this program");
  await panel.locator(".js-attach-files").setInputFiles({
    name: "setup.exe",
    mimeType: "application/octet-stream",
    buffer: Buffer.from("MZ\x90\x00"),
  });
  await panel.getByRole("button", { name: "Draft questions" }).click();
  await expect(page.getByText("setup.exe can't be used")).toBeVisible({ timeout: aiTimeout });
  await expect(page.locator('#ai-generate textarea[name="prompt"]')).toHaveValue("about this program");
});
