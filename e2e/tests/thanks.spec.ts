import { test, expect, Page } from "@playwright/test";
import { minFillWait, submitTimeout } from "./helpers";

// A creator's own thank you page (issue #9): a message and a link,
// published with the version, shown to a respondent after they send
// their answers, as text and never as markup.

async function fillThanks(page: Page, message: string, label: string, link: string) {
  const form = page.locator(".js-thanks-form");
  await form.locator('textarea[name="thanks_message"]').fill(message);
  await form.locator('input[name="thanks_link_label"]').fill(label);
  await form.locator('input[name="thanks_link_url"]').fill(link);
  await form.getByRole("button", { name: "Save thank you page" }).click();
}

test("a respondent is thanked in the creator's words, with their link", async ({ page, browser }) => {
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill(`E2E thanks ${Date.now()}`);
  await page.getByRole("button", { name: "Create survey" }).click();
  const add = page.locator('form[action$="/questions"]');
  await add.locator('select[name="type"]').selectOption("short_text");
  await add.locator('input[name="text"]').fill("What did you order?");
  await add.getByRole("button", { name: "Add question" }).click();

  // An address that is not a web page is refused, and what was typed
  // stays in the form.
  await fillThanks(page, "Thanks for dining with us!", "Book a table", "javascript:alert(1)");
  await expect(page.getByText("the link address must start with http:// or https://")).toBeVisible();
  await expect(page.locator('.js-thanks-form textarea[name="thanks_message"]')).toHaveValue(
    "Thanks for dining with us!",
  );

  await fillThanks(page, "Thanks for dining with us!\nSee you soon. <b>Bye</b>", "Book a table", "https://example.com/book");
  await expect(page.getByText("Thank you page saved")).toBeVisible();
  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const share = await page.locator(".js-share-link a").getAttribute("href");
  if (!share) throw new Error("no share link after publishing");

  const context = await browser.newContext({ storageState: undefined, locale: "en-US" });
  const respondent = await context.newPage();
  await respondent.goto(share);
  await respondent.getByLabel("What did you order?").fill("The soup");
  await minFillWait(respondent);
  await respondent.getByRole("button", { name: "Submit answers" }).click();
  await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible({ timeout: submitTimeout });

  const message = respondent.locator(".js-thanks-message");
  await expect(message).toContainText("Thanks for dining with us!");
  await expect(message).toContainText("See you soon. <b>Bye</b>");
  await expect(message.locator("b")).toHaveCount(0);
  await expect(message.locator("br")).toHaveCount(1);
  await expect(respondent.getByText("Your answers are in.")).toHaveCount(0);

  const link = respondent.locator(".js-thanks-link");
  await expect(link).toHaveText("Book a table");
  await expect(link).toHaveAttribute("href", "https://example.com/book");
  await expect(link).toHaveAttribute("rel", "noopener noreferrer");
  await context.close();
});
