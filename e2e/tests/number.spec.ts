import { test, expect, Page } from "@playwright/test";
import { minFillWait, submitTimeout } from "./helpers";

// A number question (issue #8) is the browser's own number field, held to
// limits the creator sets. Typing into it must reach the field: its
// digits are the answer, not the keys that pick a rating, which the next
// question uses.

async function numberSurvey(page: Page, title: string): Promise<string> {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(title);
  await page.getByRole("button", { name: "Create survey" }).click();

  const add = page.locator('form[action$="/questions"]');
  await add.locator('select[name="type"]').selectOption("number");
  await add.locator('input[name="text"]').fill("How many people did you come with?");
  await add.getByLabel("Lowest answer").fill("1");
  await add.getByLabel("Highest answer").fill("12");
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

test("a number question is answered by typing digits into its field", async ({ page, browser }) => {
  const share = await numberSurvey(page, `E2E number ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);

  const number = respondent.getByLabel("How many people did you come with?");
  await expect(number).toHaveAttribute("type", "number");
  await expect(number).toHaveAttribute("min", "1");
  await expect(number).toHaveAttribute("max", "12");
  await number.click();
  await respondent.keyboard.type("12");
  await expect(number).toHaveValue("12");

  await respondent.keyboard.press("Enter");
  await expect(respondent.locator(".js-respond-progress")).toHaveText("Question 2 of 2");
  // The digits that were typed into the number did not answer the scale.
  await expect(respondent.locator(".js-scale-point input:checked")).toHaveCount(0);
  await respondent.keyboard.press("9");
  await expect(respondent.locator('.js-scale-point input[value="9"]')).toBeChecked();

  await minFillWait(respondent);
  await respondent.keyboard.press("Enter");
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible({
    timeout: submitTimeout,
  });
  await context.close();

  // The creator reads the answer in the summary.
  await page.goto("/surveys/" + share.split("/").pop() + "/results");
  await expect(page.getByText("lowest 12 · highest 12").first()).toBeVisible();
});
