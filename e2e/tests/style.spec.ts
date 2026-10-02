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

// The survey's own header and footer: whose survey it is, above the
// questions, and the creator's footer above Earful's. The whole header
// is read once; past the first question it is the name alone, so the
// question stays in view. The name is not a heading, so the survey's
// title is still the page's first.
test("a survey's header heads the questions, shrinks to its name after the first, and is axe clean", async ({
  page,
  browser,
}) => {
  const share = await createPublishedSurvey(page, `E2E header ${Date.now()}`);
  const editor = "/surveys/" + share.split("/").pop();

  await page.goto(editor + "/style");
  const header = page.locator(".js-style-header-panel");
  const footer = page.locator(".js-style-footer-panel");
  await header.getByLabel(/^Name/).fill("Corner Workshop");
  await header.getByLabel(/^Tagline/).fill("Evening classes in wood and clay.");
  await header.getByLabel("Label 1", { exact: true }).fill("Our classes");
  await header.getByLabel("Address 1", { exact: true }).fill("https://example.com/classes");
  await footer.getByLabel(/^Text/).fill("Corner Workshop Cooperative");
  await footer.getByLabel("Label 1", { exact: true }).fill("Privacy notice");
  await footer.getByLabel("Address 1", { exact: true }).fill("https://example.com/privacy");
  await page.getByRole("button", { name: "Save style" }).click();
  await expect(page.getByText("Style saved")).toBeVisible();
  await page.goto(editor);
  await page.getByRole("button", { name: "Publish version 2" }).click();
  await expect(page.getByText("Published version 2")).toBeVisible();

  for (const scheme of ["light", "dark"] as const) {
    const context = await browser.newContext({ storageState: undefined, colorScheme: scheme });
    const respondent = await context.newPage();
    await respondent.goto(share);
    const shown = respondent.locator(".js-style-header");
    await expect(shown.locator(".js-style-name")).toHaveText("Corner Workshop");
    await expect(shown.locator(".js-style-tagline")).toBeVisible();
    await expect(shown.getByRole("link", { name: /Our classes/ })).toHaveAttribute("target", "_blank");
    await expect(respondent.locator(".js-style-footer")).toContainText("Corner Workshop Cooperative");
    await expect(respondent.getByRole("heading").first()).toHaveJSProperty("tagName", "H1");
    await expect(respondent.locator(".js-made-with")).toContainText("Powered by Earful");
    const results = await new AxeBuilder({ page: respondent }).analyze();
    expect(results.violations, `header in ${scheme} mode`).toEqual([]);

    // One question at a time: the second is under the name alone.
    await respondent.locator("textarea").fill("More evenings.");
    await respondent.getByRole("button", { name: "Next" }).click();
    await expect(shown.locator(".js-style-name")).toBeVisible();
    await expect(shown.locator(".js-style-tagline")).toBeHidden();
    await expect(shown.getByRole("link", { name: /Our classes/ })).toBeHidden();
    await context.close();
  }
});
