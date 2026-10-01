import { test, expect, Page } from "@playwright/test";
import { minFillWait, submitTimeout } from "./helpers";

// A date question (issue #6) is the browser's own date control. Typing
// into it must reach the field: its digits are the day, month and year,
// not the keys that pick a rating, which the next question uses.

async function dateSurvey(page: Page, title: string): Promise<string> {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(title);
  await page.getByRole("button", { name: "Create survey" }).click();

  const add = page.locator('form[action$="/questions"]');
  await add.locator('select[name="type"]').selectOption("date");
  await add.locator('input[name="text"]').fill("When did you visit?");
  await add.getByRole("button", { name: "Add question" }).click();
  await add.locator('select[name="type"]').selectOption("nps");
  await add.locator('input[name="text"]').fill("How likely are you to recommend us?");
  await add.getByRole("button", { name: "Add question" }).click();

  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const share = await page.locator(".js-share-link a").getAttribute("href");
  if (!share) throw new Error("no share link after publishing");
  return share;
}

test("a date question is answered with the browser's date control", async ({ page, browser }) => {
  const share = await dateSurvey(page, `E2E date ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined, locale: "en-US" });
  const respondent = await context.newPage();
  await respondent.goto(share);

  const date = respondent.getByLabel("When did you visit?");
  await expect(date).toHaveAttribute("type", "date");
  // The order of the control's segments follows the machine's region,
  // not the page's locale, so typed digits are only checked to reach the
  // field; the day itself is then set whole.
  await date.focus();
  await respondent.keyboard.type("04182026");
  await expect(date).toHaveValue(/^2026-\d\d-\d\d$/);
  await date.fill("2026-04-18");

  await respondent.keyboard.press("Enter");
  await expect(respondent.locator(".js-respond-progress")).toHaveText("Question 2 of 2");
  // The digit that was typed into the date did not answer the scale.
  await expect(respondent.locator(".js-scale-point input:checked")).toHaveCount(0);
  await respondent.keyboard.press("9");
  await expect(respondent.locator('.js-scale-point input[value="9"]')).toBeChecked();

  await minFillWait(respondent);
  await respondent.keyboard.press("Enter");
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible({
    timeout: submitTimeout,
  });
  await context.close();

  // The creator reads the day, in words, in the results.
  await page.goto("/surveys/" + share.split("/").pop() + "/results");
  await expect(page.getByText("18 April 2026").first()).toBeVisible();
});
