import { test, expect, Page } from "@playwright/test";
import { minFillWait, submitTimeout } from "./helpers";

// Other with a box to write in (issue #21). Picking Other puts the
// cursor in its box, and writing in the box picks Other, so the two read
// as one answer. Without a script the server makes the same connection,
// which the HTTP tests cover.

async function otherSurvey(page: Page, title: string): Promise<string> {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(title);
  await page.getByRole("button", { name: "Create survey" }).click();

  const add = page.locator('form[action$="/questions"]');
  await add.locator('select[name="type"]').selectOption("single_choice");
  await add.locator('input[name="text"]').fill("How did you hear of us?");
  await add.locator('textarea[name="options"]').fill("Email\nA friend");
  await add.locator(".js-allow-other").check();
  await add.getByRole("button", { name: "Add question" }).click();
  await add.locator('select[name="type"]').selectOption("multiple_choice");
  await add.locator('input[name="text"]').fill("How do you keep in touch?");
  await add.locator('textarea[name="options"]').fill("Email\nPhone");
  await add.locator(".js-allow-other").check();
  await add.getByRole("button", { name: "Add question" }).click();
  // The box shows only for the types with options.
  await add.locator('select[name="type"]').selectOption("long_text");
  await expect(add.locator(".js-allow-other")).toBeHidden();

  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const share = await page.locator(".js-share-link a").getAttribute("href");
  if (!share) throw new Error("no share link after publishing");
  return share;
}

test("a respondent answers Other in their own words", async ({ page, browser }) => {
  const share = await otherSurvey(page, `E2E other ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  const respondent = await context.newPage();
  await respondent.goto(share);

  const first = respondent.locator(".js-respond-question").nth(0);
  await first.locator(".js-other-choice").check();
  await expect(first.locator(".js-other-text")).toBeFocused();
  await respondent.keyboard.type("A podcast");
  await respondent.getByRole("button", { name: "Next" }).click();

  // Writing in the box picks Other.
  const second = respondent.locator(".js-respond-question").nth(1);
  await second.getByLabel("Phone").check();
  await second.locator(".js-other-text").fill("Carrier pigeon");
  await expect(second.locator(".js-other-choice")).toBeChecked();

  await minFillWait(respondent);
  await respondent.getByRole("button", { name: "Submit answers" }).click();
  await expect(respondent.locator(".js-answer-summary")).toBeVisible({ timeout: submitTimeout });
  await expect(respondent.locator(".js-answer-summary")).toContainText("Other: A podcast");
  await expect(respondent.locator(".js-answer-summary")).toContainText("Phone, Other: Carrier pigeon");
  await context.close();

  await page.goto("/surveys/" + share.split("/").pop() + "/results");
  await expect(page.locator(".js-other-answer").filter({ hasText: "A podcast" })).toBeVisible();
  await expect(page.locator(".js-other-answer").filter({ hasText: "Carrier pigeon" })).toBeVisible();
});
