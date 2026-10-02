import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createPublishedSurvey } from "./helpers";

// A survey's style (ADR-0018): a theme chosen on the Style tab is seen in
// the preview, reaches respondents when it is published, and is drawn in
// the respondent's own display mode. A themed page is held to the same
// bar as Earful's own: axe reports nothing, in light mode and in dark.
test("a respondent's page is drawn in the published theme, axe clean in both display modes", async ({
  page,
  browser,
}) => {
  const share = await createPublishedSurvey(page, `E2E style ${Date.now()}`);
  const editor = "/surveys/" + share.split("/").pop();

  let version = 1;
  for (const [theme, name] of [
    ["slate", "Slate"],
    ["ocean", "Ocean"],
    ["forest", "Forest"],
  ]) {
    await page.goto(editor + "/style");
    await page.locator(".js-style-form").getByRole("radio", { name: new RegExp(name) }).check();
    await page.getByRole("button", { name: "Save style" }).click();
    await expect(page.getByText("Style saved")).toBeVisible();

    // The preview has it before any respondent does.
    await page.locator(".js-style-preview").click();
    await expect(page.locator("html")).toHaveClass(`theme-${theme}`);

    await page.goto(editor);
    version++;
    await page.getByRole("button", { name: `Publish version ${version}` }).click();
    await expect(page.getByText(`Published version ${version}`)).toBeVisible();

    for (const scheme of ["light", "dark"] as const) {
      const context = await browser.newContext({ storageState: undefined, colorScheme: scheme });
      const respondent = await context.newPage();
      await respondent.goto(share);
      await expect(respondent.locator(".js-respond-form")).toBeVisible();
      await expect(respondent.locator("html")).toHaveClass(`theme-${theme}`);
      const results = await new AxeBuilder({ page: respondent }).analyze();
      expect(results.violations, `${theme} in ${scheme} mode`).toEqual([]);
      await context.close();
    }
  }

  // The creator's own pages are never drawn in a survey's theme.
  await page.goto(editor + "/style");
  await expect(page.locator("html")).not.toHaveClass(/theme-/);
});
