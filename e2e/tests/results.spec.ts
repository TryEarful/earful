import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createPublishedSurvey, minFillWait } from "./helpers";

// Results, stats and exports in a real browser (M7).

test("results read back what respondents said, and export cleanly", async ({ page, browser }) => {
  const title = `E2E results ${Date.now()}`;
  const share = await createPublishedSurvey(page, title);

  // Two respondents answer, from their own contexts.
  for (const [text, choice] of [
    ["Setup took a while but support was quick.", "Weekly"],
    ["Loved it — nothing to change.", "Monthly"],
  ]) {
    const context = await browser.newContext({ storageState: undefined });
    const respondent = await context.newPage();
    await respondent.goto(share);
    await respondent.locator("textarea").fill(text);
    await respondent.getByRole("button", { name: "Next" }).click();
    await respondent.getByLabel(choice).check();
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: "Submit answers" }).click();
    await expect(respondent.getByRole("heading", { name: "Thank you" })).toBeVisible();
    await context.close();
  }

  // The creator reads them.
  await page.getByRole("link", { name: /Results/ }).click();
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();
  await expect(page.getByText("Setup took a while but support was quick.")).toBeVisible();
  await expect(page.getByText("Loved it — nothing to change.")).toBeVisible();

  // Stats are present and honest about what they mean.
  await expect(page.getByText("How this survey is going")).toBeVisible();
  await expect(page.getByText(/counts times the survey page was loaded/)).toBeVisible();

  // Small samples show no audience buckets at all (ADR-0009, n < 5).
  await expect(page.getByRole("heading", { name: "Audience" })).toHaveCount(0);

  // The results page is as accessible as the rest.
  const scan = await new AxeBuilder({ page }).analyze();
  expect(scan.violations).toEqual([]);

  // CSV downloads with both answers in it.
  const download = page.waitForEvent("download");
  await page.getByRole("link", { name: "Download CSV" }).click();
  const file = await download;
  const stream = await file.createReadStream();
  const csv = (await new Response(stream as any).text()) as string;
  expect(csv).toContain("response_id,version,submitted_at,duration_secs");
  expect(csv).toContain("Loved it");
});

test("a workspace export can be built and downloaded", async ({ page }) => {
  await createPublishedSurvey(page, `E2E export ${Date.now()}`);

  await page.goto("/account");
  await page.getByRole("button", { name: /Export my workspace|Build a fresh export/ }).click();

  // The build is asynchronous; the page says so, and reloading shows the
  // finished archive.
  await expect(async () => {
    await page.reload();
    await expect(page.getByRole("link", { name: "Download the archive" })).toBeVisible({
      timeout: 1000,
    });
  }).toPass({ timeout: 30000 });

  const download = page.waitForEvent("download");
  await page.getByRole("link", { name: "Download the archive" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toContain(".zip");
});
