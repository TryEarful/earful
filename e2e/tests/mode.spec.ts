import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createPublishedSurvey } from "./helpers";

// The display mode switcher (docs/style-guide.md, "Dark mode"). The
// server draws the chosen mode as data-mode on <html>; with a script the
// choice is applied at once and posted in the background.

test("choosing dark is drawn at once and survives a reload", async ({ browser }) => {
  const context = await browser.newContext({ storageState: undefined, colorScheme: "light" });
  const page = await context.newPage();
  await page.goto("/login");
  const html = page.locator("html");
  await expect(html).not.toHaveAttribute("data-mode");

  // With a script there is nothing to press: the button is hidden.
  await expect(page.locator(".js-mode-submit")).toBeHidden();
  const saved = page.waitForResponse((r) => r.url().endsWith("/mode") && r.request().method() === "POST");
  await page.getByLabel("Display mode").selectOption("dark");
  await expect(html).toHaveAttribute("data-mode", "dark");
  await expect(page).toHaveURL(/\/login$/);
  await saved;

  await page.reload();
  await expect(html).toHaveAttribute("data-mode", "dark");
  await expect(page.getByLabel("Display mode")).toHaveValue("dark");
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);

  // Following the system again forgets it.
  const forgotten = page.waitForResponse((r) => r.url().endsWith("/mode") && r.request().method() === "POST");
  await page.getByLabel("Display mode").selectOption("system");
  await expect(html).not.toHaveAttribute("data-mode");
  await forgotten;
  await page.reload();
  await expect(html).not.toHaveAttribute("data-mode");
  await context.close();
});

test("the display mode is chosen without JavaScript", async ({ browser }) => {
  const context = await browser.newContext({ storageState: undefined, javaScriptEnabled: false, colorScheme: "dark" });
  const page = await context.newPage();
  await page.goto("/help");
  await page.getByLabel("Display mode").selectOption("light");
  await page.locator(".js-mode-submit").click();
  await expect(page).toHaveURL(/\/help$/);
  await expect(page.locator("html")).toHaveAttribute("data-mode", "light");
  await page.goto("/trust");
  await expect(page.locator("html")).toHaveAttribute("data-mode", "light");
  await context.close();
});

// On a survey the choice must not cost the respondent what they typed.
test("a respondent changes the display mode without losing an answer", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E mode ${Date.now()}`);
  const context = await browser.newContext({ storageState: undefined, colorScheme: "light" });
  const respondent = await context.newPage();
  await respondent.goto(share);
  await respondent.locator("textarea").fill("Typed before the lights went down.");

  const saved = respondent.waitForResponse((r) => r.url().endsWith("/mode") && r.request().method() === "POST");
  await respondent.getByLabel("Display mode").selectOption("dark");
  await expect(respondent.locator("html")).toHaveAttribute("data-mode", "dark");
  await saved;
  await expect(respondent.locator("textarea")).toHaveValue("Typed before the lights went down.");

  await respondent.reload();
  await expect(respondent.locator("html")).toHaveAttribute("data-mode", "dark");
  await context.close();
});
