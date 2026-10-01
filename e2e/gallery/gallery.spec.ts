import { test, expect, type Browser, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { execFileSync } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import { createPublishedSurvey, latestLinkTo, minFillWait, signIn, submitTimeout, uniqueEmail } from "../tests/helpers";

// The design gallery: every page, at a phone's width and a desktop's, in
// the light theme and the dark, in English and Spanish, with an axe
// report beside each picture. It asserts nothing about the product; it
// is how the design is looked at as a whole (docs/style-guide.md). Run it
// with `make gallery`; the pictures land in GALLERY_DIR.

const out = process.env.GALLERY_DIR ?? path.join(__dirname, "..", "test-results", "gallery");
const viewports = [
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 900 },
];
const themes = ["light", "dark"] as const;

type Axe = { page: string; lang: string; violations: { id: string; impact: string | null; nodes: number; help: string }[] };
const axe: Axe[] = [];

async function capture(page: Page, name: string, lang: string) {
  fs.mkdirSync(out, { recursive: true });
  const url = page.url();
  for (const theme of themes) {
    await page.emulateMedia({ colorScheme: theme });
    for (const v of viewports) {
      await page.setViewportSize({ width: v.width, height: v.height });
      await page.waitForTimeout(150);
      await page.screenshot({ path: path.join(out, `${name}.${lang}.${v.name}.${theme}.png`), fullPage: true });
    }
    const result = await new AxeBuilder({ page }).analyze();
    axe.push({
      page: `${name} (${theme})`,
      lang,
      violations: result.violations.map((x) => ({ id: x.id, impact: x.impact ?? null, nodes: x.nodes.length, help: x.help })),
    });
  }
  await page.emulateMedia({ colorScheme: "light" });
  if (page.url() !== url) await page.goto(url);
}

async function visitor(browser: Browser, lang: string) {
  const context = await browser.newContext({ locale: lang === "es" ? "es-ES" : "en-US" });
  return { context, page: await context.newPage() };
}

test.setTimeout(15 * 60_000);

test("gallery", async ({ browser }) => {
  // Pages anyone can reach, in both languages.
  for (const lang of ["en", "es"]) {
    const { context, page } = await visitor(browser, lang);
    for (const [name, url] of [
      ["home", "/"],
      ["login", "/login"],
      ["help", "/help"],
      ["help-voice", "/help/voice"],
      ["trust", "/trust"],
      ["magic-invalid", "/auth/magic/verify?token=not-a-token"],
      ["survey-missing", "/s/00000000-0000-0000-0000-000000000000"],
      // After an account closes, without and with a copy of the data
      // asked for.
      ["goodbye", "/goodbye"],
      ["goodbye-copy", "/goodbye?copy=sent"],
      ["closure-missing", "/exports/closure/not-a-token"],
    ]) {
      await page.goto(url);
      await capture(page, name, lang);
    }
    await context.close();
  }

  // A creator: signing in, the dashboard, building and publishing.
  const { context: creatorContext, page } = await visitor(browser, "en");
  const email = uniqueEmail("gallery");
  await page.goto("/login");
  await page.getByLabel("Email address").fill(email);
  await page.getByRole("button", { name: "Email me a link to sign in" }).click();
  await expect(page.getByRole("heading", { name: "Check your email" })).toBeVisible();
  await capture(page, "magic-sent", "en");
  const link = await latestLinkTo(email, /https?:\/\/[^\s]+\/auth\/magic\/verify\?token=[\w-]+/);
  await page.goto(link);
  await capture(page, "magic-confirm", "en");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard$/);

  await capture(page, "dashboard", "en");
  await page.goto("/surveys/new");
  await capture(page, "survey-new", "en");

  const title = "How was the workshop?";
  const share = await createPublishedSurvey(page, title);
  // The editor is drawn in answer to the publish POST, so its address is
  // built from the survey's ID rather than read from the address bar.
  const editor = "/surveys/" + share.split("/").pop();
  await capture(page, "editor-published", "en");
  // The preview, one question at a time and every question on one page.
  await page.goto(editor + "/preview");
  await capture(page, "preview", "en");
  await page.goto(editor + "/preview?layout=all");
  await capture(page, "preview-all", "en");
  await page.goto("/dashboard");
  await capture(page, "dashboard-surveys", "en");
  // The account page ends with the delete form and its copy checkbox.
  await page.goto("/account");
  await capture(page, "account", "en");
  await page.goto("/help");
  await capture(page, "help-signed-in", "en");

  // The preview, submitted: nothing recorded, the answers read back.
  await page.goto(editor + "/preview");
  await page.locator("textarea").fill("Shorter sessions and more time to try things ourselves.");
  const next = page.getByRole("button", { name: "Next" });
  if (await next.isVisible()) await next.click();
  await page.getByLabel(/Monthly/).check();
  await page.getByRole("button", { name: "Submit answers" }).click();
  await expect(page.locator(".js-answer-summary")).toBeVisible();
  await capture(page, "preview-submitted", "en");

  // A draft, before anything is published.
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill("Team offsite ideas");
  await page.getByRole("button", { name: "Create survey" }).click();
  await capture(page, "editor-draft", "en");

  // A respondent answers, in each language.
  for (const lang of ["en", "es"]) {
    const { context, page: respondent } = await visitor(browser, lang);
    await respondent.goto(share);
    await capture(respondent, "respond-first", lang);
    await respondent.locator("textarea").fill("Shorter sessions and more time to try things ourselves.");
    await respondent.getByRole("button", { name: lang === "es" ? "Siguiente" : "Next" }).click();
    await respondent.getByLabel(/Monthly/).check();
    await capture(respondent, "respond-last", lang);
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    await expect(respondent.locator("h1")).toBeVisible({ timeout: submitTimeout });
    await expect(respondent.locator(".js-answer-summary")).toBeVisible();
    await capture(respondent, "respond-thanks-with-answers", lang);
    await context.close();
  }

  // What the creator reads afterwards.
  await page.goto(editor + "/results");
  await capture(page, "results", "en");
  await page.goto(editor + "/stats");
  await capture(page, "stats", "en");
  await page.goto(editor + "/localizations");
  await capture(page, "languages", "en");
  // A language added and drafted, so the translation view and how a
  // machine draft is marked are looked at too.
  await page.locator('input[name="lang"]').fill("nl");
  await page.getByRole("button", { name: "Add language" }).click();
  const draft = page.getByRole("button", { name: "Draft the missing translations with AI" });
  if (await draft.count()) {
    await draft.first().click();
    await page.waitForLoadState("networkidle");
  }
  await capture(page, "languages-added", "en");
  await page.goto(editor);
  await capture(page, "editor-after-answers", "en");

  // A survey asking for a date: the browser's own date control, the
  // message for a required date left empty, and the days in the results.
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill("Your visit");
  await page.getByRole("button", { name: "Create survey" }).click();
  const addDate = page.locator('form[action$="/questions"]');
  await addDate.locator('select[name="type"]').selectOption("date");
  await addDate.locator('input[name="text"]').fill("When did you visit?");
  await addDate.getByLabel("Required").check();
  await addDate.getByRole("button", { name: "Add question" }).click();
  await capture(page, "editor-date", "en");
  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const dateShare = await page.locator(".js-share-link a").getAttribute("href");
  if (!dateShare) throw new Error("no share link after publishing");
  for (const lang of ["en", "es"]) {
    const { context, page: respondent } = await visitor(browser, lang);
    await respondent.goto(dateShare);
    await capture(respondent, "respond-date", lang);
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    const needed = lang === "es" ? "esta pregunta necesita una respuesta" : "this question needs an answer";
    await expect(respondent.getByText(needed).first()).toBeVisible({ timeout: submitTimeout });
    await capture(respondent, "respond-date-error", lang);
    await respondent.getByLabel("When did you visit?").fill("2026-04-18");
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    await expect(respondent.locator("h1")).toBeVisible({ timeout: submitTimeout });
    await context.close();
  }
  const dateEditor = "/surveys/" + dateShare.split("/").pop();
  await page.goto(dateEditor + "/results");
  await capture(page, "results-date", "en");

  // The creator in Spanish.
  const { context: spanish, page: es } = await visitor(browser, "es");
  await spanish.addCookies(await creatorContext.cookies());
  await es.goto("/dashboard");
  await capture(es, "dashboard", "es");
  await es.goto(editor);
  await capture(es, "editor-published", "es");
  await es.goto(editor + "/preview");
  await capture(es, "preview", "es");
  await es.goto(editor + "/preview?layout=all");
  await capture(es, "preview-all", "es");
  await es.goto(editor + "/results");
  await capture(es, "results", "es");
  await es.goto(editor + "/stats");
  await capture(es, "stats", "es");
  await es.goto("/account");
  await capture(es, "account", "es");

  // The AI tier control, a super admin's page. Only the CLI grants super
  // admin, so the creator is granted it inside the compose stack's app
  // container; against any other base URL there is no container to ask.
  if (!process.env.E2E_BASE_URL) {
    execFileSync("docker", ["compose", "exec", "-T", "app", "/earful", "admin", "grant", email], {
      cwd: path.join(__dirname, "..", ".."),
    });
    await page.goto("/admin/ai-tiers");
    await capture(page, "admin-ai-tiers", "en");
    await page.goto("/admin/ai-tiers?email=" + encodeURIComponent(uniqueEmail("nobody")));
    await capture(page, "admin-ai-tiers-none", "en");
    await page.goto("/admin/ai-tiers?email=" + encodeURIComponent(email));
    await capture(page, "admin-ai-tiers-found", "en");
    await page.locator(".js-tier").first().selectOption("high");
    await page.locator(".js-tier-form").first().locator("button").click();
    await expect(page).toHaveURL(/notice=saved/);
    await capture(page, "admin-ai-tiers-saved", "en");
    // A tier the server does not know, as a tampered form would send it.
    await page.locator(".js-tier").first().evaluate((select: HTMLSelectElement) => {
      select.add(new Option("unlimited", "unlimited", true, true));
    });
    await page.locator(".js-tier-form").first().locator("button").click();
    await capture(page, "admin-ai-tiers-error", "en");
    const { context: spanishAdmin, page: esAdmin } = await visitor(browser, "es");
    await spanishAdmin.addCookies(await creatorContext.cookies());
    await esAdmin.goto("/admin/ai-tiers?email=" + encodeURIComponent(email));
    await capture(esAdmin, "admin-ai-tiers-found", "es");
    await esAdmin.goto("/account");
    await capture(esAdmin, "account-admin", "es");
    await spanishAdmin.close();
  }

  await spanish.close();
  await creatorContext.close();

  fs.writeFileSync(path.join(out, "axe.json"), JSON.stringify(axe, null, 2));
});
