import { test, expect } from "@playwright/test";
import { aiTimeout, offersAIDrafting, scriptedAI } from "./helpers";

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

  await panel.locator('textarea[name="prompt"]').fill("how our first week feels to a new customer");
  await panel.getByRole("button", { name: "Draft questions" }).click();

  // Output appears while the model is still talking, ends with the
  // summary, and the editor then shows the questions themselves. The
  // first fragment is where a real model's thinking time lands, so this
  // gets the AI budget rather than the default 5s.
  await expect(panel.locator(".generate-output")).not.toBeEmpty({ timeout: aiTimeout });
  await expect(panel.locator(".generate-output")).toContainText(/Added \d+ questions?/, {
    timeout: aiTimeout,
  });

  // They are ordinary draft questions: listed, and editable.
  const questions = page.locator(".questions li.question");
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
  expect(await page.locator(".questions li.question").count()).toBeGreaterThan(2);

  await context.close();
});
