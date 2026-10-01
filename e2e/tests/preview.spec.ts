import { test, expect } from "@playwright/test";
import { createPublishedSurvey } from "./helpers";

// Preview pages one question at a time, as a respondent sees it, and
// offers every question on one page so a creator can read a long survey
// without stepping through it (?layout=all). The switch is a link, so it
// works with JavaScript off too.
test("preview shows every question on one page with ?layout=all", async ({ page }) => {
  const share = await createPublishedSurvey(page, `E2E preview layout ${Date.now()}`);
  const preview = "/surveys/" + share.split("/").pop() + "/preview";
  const first = page.getByText("What would make surveys less painful?");
  const second = page.getByText("How often do you answer surveys?");

  await page.goto(preview);
  await expect(first).toBeVisible();
  await expect(second).toBeHidden();

  await page.locator(".js-preview-layout").click();
  await expect(page).toHaveURL(/\/preview\?layout=all$/);
  await expect(first).toBeVisible();
  await expect(second).toBeVisible();
  await expect(page.getByRole("button", { name: "Next" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Submit answers" })).toBeVisible();

  await page.locator(".js-preview-layout").click();
  await expect(page).toHaveURL(/\/preview$/);
  await expect(first).toBeVisible();
  await expect(second).toBeHidden();
});
