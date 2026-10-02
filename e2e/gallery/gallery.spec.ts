import { test, expect, type Browser, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { execFileSync } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";
import {
  aiTimeout,
  createPublishedSurvey,
  fakeMicrophone,
  latestLinkTo,
  minFillWait,
  offersAIDrafting,
  offersSurveyFromDescription,
  offersVoice,
  signIn,
  submitTimeout,
  uniqueEmail,
} from "../tests/helpers";

// The design gallery: every page, at a phone's width and a desktop's, in
// light mode and dark, in English and Spanish, with an axe
// report beside each picture; a few pages also with a display mode chosen
// against the system's. It asserts nothing about the product; it
// is how the design is looked at as a whole (docs/style-guide.md). Run it
// with `make gallery`; the pictures land in GALLERY_DIR.

const out = process.env.GALLERY_DIR ?? path.join(__dirname, "..", "test-results", "gallery");
const viewports = [
  { name: "phone", width: 390, height: 844 },
  { name: "desktop", width: 1280, height: 900 },
];
const modes = ["light", "dark"] as const;

type Axe = { page: string; lang: string; violations: { id: string; impact: string | null; nodes: number; help: string }[] };
const axe: Axe[] = [];

async function capture(page: Page, name: string, lang: string) {
  fs.mkdirSync(out, { recursive: true });
  const url = page.url();
  for (const mode of modes) {
    await page.emulateMedia({ colorScheme: mode });
    for (const v of viewports) {
      await page.setViewportSize({ width: v.width, height: v.height });
      await page.waitForTimeout(150);
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.screenshot({ path: path.join(out, `${name}.${lang}.${v.name}.${mode}.png`), fullPage: true });
    }
    const result = await new AxeBuilder({ page }).analyze();
    axe.push({
      page: `${name} (${mode})`,
      lang,
      violations: result.violations.map((x) => ({ id: x.id, impact: x.impact ?? null, nodes: x.nodes.length, help: x.help })),
    });
  }
  await page.emulateMedia({ colorScheme: "light" });
  if (page.url() !== url) await page.goto(url);
}

// A display mode chosen with the switcher is drawn against the system's: dark
// on a light system and light on a dark one. Every token a mode sets
// must then win over the system's, so these pages are pictured and
// scanned that way too. The pictures are named for the chosen mode.
const forced = [
  { chosen: "dark", system: "light" },
  { chosen: "light", system: "dark" },
] as const;

async function captureForced(page: Page, name: string, lang: string) {
  fs.mkdirSync(out, { recursive: true });
  const context = page.context();
  const url = page.url();
  for (const { chosen, system } of forced) {
    await context.addCookies([{ name: "mode", value: chosen, url: new URL(url).origin }]);
    await page.emulateMedia({ colorScheme: system });
    await page.goto(url);
    for (const v of viewports) {
      await page.setViewportSize({ width: v.width, height: v.height });
      await page.waitForTimeout(150);
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.screenshot({ path: path.join(out, `${name}.${lang}.${v.name}.chosen-${chosen}.png`), fullPage: true });
    }
    const result = await new AxeBuilder({ page }).analyze();
    axe.push({
      page: `${name} (${chosen} chosen on a ${system} system)`,
      lang,
      violations: result.violations.map((x) => ({ id: x.id, impact: x.impact ?? null, nodes: x.nodes.length, help: x.help })),
    });
  }
  await context.clearCookies({ name: "mode" });
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto(url);
}

async function visitor(browser: Browser, lang: string) {
  const context = await browser.newContext({ locale: lang === "es" ? "es-ES" : "en-US" });
  return { context, page: await context.newPage() };
}

// The header and footer of a survey's style, typed into the Style tab.
// A link is a label and an address; rows left out are emptied.
type StyleWords = { name?: string; tagline?: string; links?: string[][]; footer?: string; footerLinks?: string[][] };

async function fillStyle(page: Page, words: StyleWords) {
  const header = page.locator(".js-style-header-panel");
  const footer = page.locator(".js-style-footer-panel");
  await header.getByLabel(/^Name/).fill(words.name ?? "");
  await header.getByLabel(/^Tagline/).fill(words.tagline ?? "");
  await footer.getByLabel(/^Text/).fill(words.footer ?? "");
  for (const [panel, links] of [[header, words.links ?? []], [footer, words.footerLinks ?? []]] as const) {
    for (let row = 0; row < 3; row++) {
      await panel.getByLabel(`Label ${row + 1}`, { exact: true }).fill(links[row]?.[0] ?? "");
      await panel.getByLabel(`Address ${row + 1}`, { exact: true }).fill(links[row]?.[1] ?? "");
    }
  }
}

test.setTimeout(30 * 60_000);

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
    await page.goto("/login");
    await captureForced(page, "login", lang);
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
  await captureForced(page, "dashboard-surveys", "en");
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

  // The drafting panel with files chosen, and with a file it refused
  // (issue #5). The refusal hands the prompt back as typed.
  if (await offersAIDrafting(page)) {
    const panel = page.locator("#ai-generate");
    await panel.locator('textarea[name="prompt"]').fill("Use the attached notes and answers");
    await panel.locator(".js-attach-files").setInputFiles([
      { name: "notes.md", mimeType: "text/markdown", buffer: Buffer.from("# Onboarding\n") },
      { name: "answers.csv", mimeType: "text/csv", buffer: Buffer.from("question,answer\n") },
    ]);
    await capture(page, "editor-files-chosen", "en");
    await panel.locator('textarea[name="prompt"]').fill("Use the attached notes and answers");
    await panel.locator(".js-attach-files").setInputFiles({
      name: "setup.exe",
      mimeType: "application/octet-stream",
      buffer: Buffer.from("MZ"),
    });
    await panel.getByRole("button", { name: "Draft questions" }).click();
    await expect(page.getByText("setup.exe can't be used")).toBeVisible({ timeout: aiTimeout });
    await capture(page, "editor-file-refused", "en");
  }

  // With text AI configured the form also takes a description, and the
  // title may be left empty. Sent with neither, it comes back with its
  // error; sent with a description, it opens the drafted survey.
  await page.goto("/surveys/new");
  if (await offersSurveyFromDescription(page)) {
    await page.getByRole("button", { name: "Create survey" }).click();
    await capture(page, "survey-new-error", "en");
    await page.goto("/surveys/new");
    await page.locator(".js-new-survey-prompt").fill("the first week of new customers");
    await capture(page, "survey-new-described", "en");
    await page.getByRole("button", { name: "Create survey" }).click();
    await expect(page).toHaveURL(/\?added=\d+/, { timeout: aiTimeout + 5000 });
    await capture(page, "editor-from-description", "en");
  }

  // A respondent answers, in each language.
  for (const lang of ["en", "es"]) {
    const { context, page: respondent } = await visitor(browser, lang);
    await respondent.goto(share);
    await capture(respondent, "respond-first", lang);
    await captureForced(respondent, "respond-first", lang);
    await respondent.locator("textarea").fill("Shorter sessions and more time to try things ourselves.");
    await respondent.getByRole("button", { name: lang === "es" ? "Siguiente" : "Next" }).click();
    await respondent.getByLabel(/Monthly/).check();
    await capture(respondent, "respond-last", lang);
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    await expect(respondent.locator("h1")).toBeVisible({ timeout: submitTimeout });
    await expect(respondent.locator(".js-answer-summary")).toBeVisible();
    await capture(respondent, "respond-thanks-with-answers", lang);
    // The same browser comes back: the page says it has answered before.
    await respondent.goto(share);
    await capture(respondent, "respond-again", lang);
    await context.close();
  }

  // A respondent whose browser asks for the other language chooses this
  // one, which the survey has no translation into: the page is worded in
  // it, the questions are as written, and a notice says so.
  for (const lang of ["en", "es"]) {
    const { context, page: respondent } = await visitor(browser, lang === "es" ? "en" : "es");
    await respondent.goto(share + "?lang=" + lang);
    await expect(respondent.locator(".js-untranslated-notice")).toBeVisible();
    await capture(respondent, "respond-language-banner", lang);
    await context.close();
  }

  // What the creator reads afterwards.
  await page.goto(editor + "/results");
  await capture(page, "results", "en");
  await captureForced(page, "results", "en");
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

  // A survey asking for a number: the limits in the editor, the field
  // with its limits said under it, the message for a number outside
  // them, and the summary in the results.
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill("Your group");
  await page.getByRole("button", { name: "Create survey" }).click();
  const addNumber = page.locator('form[action$="/questions"]');
  await addNumber.locator('select[name="type"]').selectOption("number");
  await addNumber.locator('input[name="text"]').fill("How many people did you come with?");
  await addNumber.getByLabel("Lowest answer").fill("1");
  await addNumber.getByLabel("Highest answer").fill("12");
  await capture(page, "editor-number-adding", "en");
  await addNumber.getByRole("button", { name: "Add question" }).click();
  await page.locator(".js-question summary").first().click();
  await capture(page, "editor-number", "en");
  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const numberShare = await page.locator(".js-share-link a").getAttribute("href");
  if (!numberShare) throw new Error("no share link after publishing");
  for (const [lang, given] of [["en", "4"], ["es", "7"]]) {
    const { context, page: respondent } = await visitor(browser, lang);
    await respondent.goto(numberShare);
    await capture(respondent, "respond-number", lang);
    const field = respondent.getByLabel("How many people did you come with?");
    await field.fill("20");
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    const outside = lang === "es" ? "escriba un número entero del 1 al 12" : "enter a whole number from 1 to 12";
    await expect(respondent.getByText(outside).first()).toBeVisible({ timeout: submitTimeout });
    await capture(respondent, "respond-number-error", lang);
    await respondent.getByLabel("How many people did you come with?").fill(given);
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    await expect(respondent.locator("h1")).toBeVisible({ timeout: submitTimeout });
    await context.close();
  }
  const numberEditor = "/surveys/" + numberShare.split("/").pop();
  await page.goto(numberEditor + "/results");
  await capture(page, "results-number", "en");

  // A choice question offering Other (issue #21): the box in the editor,
  // Other picked with words in its box, the message for Other picked
  // with nothing written, and what was written listed in the results.
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill("Finding us");
  await page.getByRole("button", { name: "Create survey" }).click();
  const addOther = page.locator('form[action$="/questions"]');
  await addOther.locator('select[name="type"]').selectOption("single_choice");
  await addOther.locator('input[name="text"]').fill("How did you hear of us?");
  await addOther.locator('textarea[name="options"]').fill("Email\nA friend\nSocial media");
  await addOther.locator(".js-allow-other").check();
  await addOther.getByLabel("Required").check();
  await capture(page, "editor-other-adding", "en");
  await addOther.getByRole("button", { name: "Add question" }).click();
  await page.locator(".js-question summary").first().click();
  await capture(page, "editor-other", "en");
  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const otherShare = await page.locator(".js-share-link a").getAttribute("href");
  if (!otherShare) throw new Error("no share link after publishing");
  for (const [lang, written] of [["en", "A podcast"], ["es", "Un pódcast"]]) {
    const { context, page: respondent } = await visitor(browser, lang);
    await respondent.goto(otherShare);
    await respondent.locator(".js-other-choice").check();
    await respondent.locator(".js-other-text").fill(written);
    await capture(respondent, "respond-other", lang);
    await respondent.locator(".js-other-text").fill("");
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    const empty = lang === "es" ? "escriba su respuesta en el espacio de Otro" : "write your answer in the box for Other";
    await expect(respondent.getByText(new RegExp(empty, "i")).first()).toBeVisible({ timeout: submitTimeout });
    await capture(respondent, "respond-other-error", lang);
    await respondent.locator(".js-other-text").fill(written);
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    await expect(respondent.locator(".js-answer-summary")).toBeVisible({ timeout: submitTimeout });
    await capture(respondent, "respond-thanks-other", lang);
    await context.close();
  }
  const otherEditor = "/surveys/" + otherShare.split("/").pop();
  await page.goto(otherEditor + "/results");
  await capture(page, "results-other", "en");
  // A thank you page of the creator's own: the editor's section refusing
  // an address that is not a web page, then saved; the page a respondent
  // reads after sending, in each language; and its translation beside
  // the questions'.
  await page.goto("/surveys/new");
  await page.getByLabel("Title").fill("Dinner at the corner");
  await page.getByRole("button", { name: "Create survey" }).click();
  const addThanksQuestion = page.locator('form[action$="/questions"]');
  await addThanksQuestion.locator('select[name="type"]').selectOption("short_text");
  await addThanksQuestion.locator('input[name="text"]').fill("What did you order?");
  await addThanksQuestion.getByRole("button", { name: "Add question" }).click();
  const thanksForm = page.locator(".js-thanks-form");
  await thanksForm.locator('textarea[name="thanks_message"]').fill(
    "Thanks for dining with us!\nWe read every answer.\n\nSee you soon at the corner.",
  );
  await thanksForm.locator('input[name="thanks_link_label"]').fill("Book a table");
  await thanksForm.locator('input[name="thanks_link_url"]').fill("javascript:alert(1)");
  await thanksForm.getByRole("button", { name: "Save thank you page" }).click();
  await expect(page.getByText("the link address must start with http:// or https://")).toBeVisible();
  await capture(page, "editor-thanks-error", "en");
  await page.locator('.js-thanks-form input[name="thanks_link_url"]').fill("https://example.com/book");
  await page.locator(".js-thanks-form").getByRole("button", { name: "Save thank you page" }).click();
  await expect(page.getByText("Thank you page saved")).toBeVisible();
  await capture(page, "editor-thanks", "en");
  await page.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published version 1")).toBeVisible();
  const thanksShare = await page.locator(".js-share-link a").getAttribute("href");
  if (!thanksShare) throw new Error("no share link after publishing");
  const thanksEditor = "/surveys/" + thanksShare.split("/").pop();
  for (const lang of ["en", "es"]) {
    const { context, page: respondent } = await visitor(browser, lang);
    await respondent.goto(thanksShare);
    await respondent.getByLabel("What did you order?").fill("The soup of the day");
    await minFillWait(respondent);
    await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
    await expect(respondent.locator(".js-thanks-message")).toBeVisible({ timeout: submitTimeout });
    await capture(respondent, "respond-thanks-custom", lang);
    await context.close();
  }
  await page.goto(thanksEditor + "/localizations");
  await page.locator('input[name="lang"]').fill("nl");
  await page.getByRole("button", { name: "Add language" }).click();
  await expect(page.locator(".js-thanks-translation")).toBeVisible();
  await capture(page, "languages-thanks", "en");

  // A survey's style (ADR-0018). The Style tab as it starts, with a theme
  // it refused and with one saved; then the survey's pages in each theme,
  // in both languages. The questions page is pictured in every theme; the
  // pages after it in Forest, beside Earful's above.
  const styledShare = await createPublishedSurvey(page, "Spring open day");
  const styledEditor = "/surveys/" + styledShare.split("/").pop();
  await page.goto(styledEditor + "/style");
  await capture(page, "style", "en");
  // A theme the server does not know, as a tampered form would send it.
  await page.locator(".js-style-form input[name=theme]").first().evaluate((radio: HTMLInputElement) => {
    radio.value = "midnight";
    radio.checked = true;
  });
  await page.getByRole("button", { name: "Save style" }).click();
  await expect(page.getByText("Choose one of the themes offered")).toBeVisible();
  await capture(page, "style-error", "en");
  let styledVersion = 1;
  for (const [theme, name] of [["slate", "Slate"], ["ocean", "Ocean"], ["forest", "Forest"]]) {
    await page.goto(styledEditor + "/style");
    await page.locator(".js-style-form").getByRole("radio", { name: new RegExp(name) }).check();
    // The survey's own header and footer go in with the first theme and
    // stay, so every themed page below is pictured with them.
    if (theme === "slate") {
      await fillStyle(page, {
        name: "Corner Workshop",
        tagline: "Evening classes in wood, clay and print.\nTell us how the open day went.",
        links: [["Our classes", "https://example.com/classes"], ["Contact", "https://example.com/contact"]],
        footer: "Corner Workshop Cooperative\n12 Mill Lane, Riverton",
        footerLinks: [["Privacy notice", "https://example.com/privacy"]],
      });
    }
    await page.getByRole("button", { name: "Save style" }).click();
    await expect(page.getByText("Style saved")).toBeVisible();
    if (theme === "ocean") await capture(page, "style-saved", "en");
    await page.goto(styledEditor);
    styledVersion++;
    await page.getByRole("button", { name: `Publish version ${styledVersion}` }).click();
    await expect(page.getByText(`Published version ${styledVersion}`)).toBeVisible();
    for (const lang of ["en", "es"]) {
      const { context, page: respondent } = await visitor(browser, lang);
      await respondent.goto(styledShare);
      await capture(respondent, `respond-first-${theme}`, lang);
      if (theme === "forest") {
        await captureForced(respondent, "respond-first-forest", lang);
        await respondent.locator("textarea").fill("Shorter sessions and more time to try things ourselves.");
        await respondent.getByRole("button", { name: lang === "es" ? "Siguiente" : "Next" }).click();
        await respondent.getByLabel(/Monthly/).check();
        await capture(respondent, "respond-last-forest", lang);
        await minFillWait(respondent);
        await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
        await expect(respondent.locator(".js-answer-summary")).toBeVisible({ timeout: submitTimeout });
        await capture(respondent, "respond-thanks-forest", lang);
        await respondent.goto(styledShare);
        await capture(respondent, "respond-again-forest", lang);
      }
      await context.close();
    }
    // A themed page with the microphone open, where Signal has to stand
    // apart from the theme's own colours. The microphone is the suite's
    // fake one, playing the recording in testdata.
    if (theme === "ocean") {
      const context = await browser.newContext({ locale: "en-US", permissions: ["microphone"] });
      await fakeMicrophone(context);
      const respondent = await context.newPage();
      await respondent.goto(styledShare);
      if (await offersVoice(respondent)) {
        await respondent.getByRole("button", { name: "Dictate" }).first().click();
        await respondent.getByRole("button", { name: "Use the microphone" }).click();
        await expect(respondent.getByRole("button", { name: "Stop", exact: true })).toBeVisible();
        await capture(respondent, "respond-recording-ocean", "en");
      }
      await context.close();
    }
  }
  // A header and a footer in Earful's own look, and where they are
  // hardest to set: the longest name, tagline and links the Style tab
  // takes, and then a name with nothing else. Before those, the tab with
  // a link it refused, the problem beside its field.
  const headedShare = await createPublishedSurvey(page, "Autumn fair feedback");
  const headedEditor = "/surveys/" + headedShare.split("/").pop();
  let headedVersion = 1;
  const headed: [string, StyleWords][] = [
    ["respond-header", {
      name: "Riverton Autumn Fair",
      tagline: "Three days of stalls, music and food by the river.",
      links: [["Programme", "https://example.com/programme"], ["Getting here", "https://example.com/travel"]],
      footer: "Riverton Fair Association",
      footerLinks: [["Privacy notice", "https://example.com/privacy"], ["Contact us", "https://example.com/contact"]],
    }],
    ["respond-header-long", {
      name: "The Riverton and District Association for Markets, Fairs and Open Air Events",
      tagline:
        "Every autumn we fill the meadow by the river with stalls, music, food and workshops for three days.\n" +
        "We ask everyone who came what to keep, what to change and what to drop, and we read every answer before planning the next one.",
      links: [
        ["The full programme for all three days", "https://example.com/programme/autumn/full"],
        ["How to get here by train, bus or bicycle", "https://example.com/travel"],
        ["Become a stallholder or a volunteer", "https://example.com/join"],
      ],
      footer:
        "The Riverton and District Association for Markets, Fairs and Open Air Events\n" +
        "Registered charity. The Old Mill, 12 Mill Lane, Riverton\n" +
        "Answers are read by the organising committee and kept for one year.",
      footerLinks: [
        ["How we look after your answers", "https://example.com/privacy"],
        ["Write to the committee", "https://example.com/contact"],
        ["Accessibility at the fair", "https://example.com/access"],
      ],
    }],
    ["respond-header-name", { name: "Riverton Autumn Fair" }],
  ];
  await page.goto(headedEditor + "/style");
  await fillStyle(page, { ...headed[0][1], links: [["Programme", ""]] });
  await page.getByRole("button", { name: "Save style" }).click();
  await expect(page.locator(".js-style-field-error")).toBeVisible();
  await capture(page, "style-field-error", "en");
  for (const [name, words] of headed) {
    await page.goto(headedEditor + "/style");
    await fillStyle(page, words);
    if (name === "respond-header-long") await capture(page, "style-filled", "en");
    await page.getByRole("button", { name: "Save style" }).click();
    await expect(page.getByText("Style saved")).toBeVisible();
    await page.goto(headedEditor);
    headedVersion++;
    await page.getByRole("button", { name: `Publish version ${headedVersion}` }).click();
    await expect(page.getByText(`Published version ${headedVersion}`)).toBeVisible();
    for (const lang of ["en", "es"]) {
      const { context, page: respondent } = await visitor(browser, lang);
      await respondent.goto(headedShare);
      await capture(respondent, name, lang);
      if (name === "respond-header-long") {
        // Past the first question the header is the name alone.
        await respondent.locator("textarea").fill("More places to sit down.");
        await respondent.getByRole("button", { name: lang === "es" ? "Siguiente" : "Next" }).click();
        await capture(respondent, "respond-header-long-later", lang);
        await minFillWait(respondent);
        await respondent.getByLabel(/Monthly/).check();
        await respondent.getByRole("button", { name: lang === "es" ? "Enviar respuestas" : "Submit answers" }).click();
        await expect(respondent.locator(".js-answer-summary")).toBeVisible({ timeout: submitTimeout });
        await capture(respondent, "respond-header-long-thanks", lang);
      }
      await context.close();
    }
  }

  // Closed, a survey still looks like itself: Forest here, and Earful on
  // the survey that asked for a date.
  for (const [name, closing, closedShare] of [
    ["respond-closed-forest", styledEditor, styledShare],
    ["respond-closed", dateEditor, dateShare],
  ]) {
    await page.goto(closing);
    await page.getByRole("button", { name: "Close survey" }).click();
    for (const lang of ["en", "es"]) {
      const { context, page: respondent } = await visitor(browser, lang);
      await respondent.goto(closedShare);
      await capture(respondent, name, lang);
      await context.close();
    }
  }

  // An invitation already answered, in Earful and then in Forest: the
  // page takes the style of the survey's latest version.
  if (!process.env.E2E_BASE_URL) {
    await page.goto("/surveys/new");
    await page.getByLabel("Title").fill("Team retrospective");
    await page.locator('input[name="anonymity"][value="invited"]').check();
    await page.getByRole("button", { name: "Create survey" }).click();
    const addInvited = page.locator('form[action$="/questions"]');
    await addInvited.locator('select[name="type"]').selectOption("short_text");
    await addInvited.locator('input[name="text"]').fill("What went well?");
    await addInvited.getByRole("button", { name: "Add question" }).click();
    await page.getByRole("button", { name: "Publish version 1" }).click();
    await expect(page.getByText("Published version 1")).toBeVisible();
    const invitedEditor = new URL(page.url()).pathname.replace(/\/publish$/, "");
    for (const name of ["respond-already", "respond-already-forest"]) {
      if (name === "respond-already-forest") {
        await page.goto(invitedEditor + "/style");
        await page.locator(".js-style-form").getByRole("radio", { name: /Forest/ }).check();
        await page.getByRole("button", { name: "Save style" }).click();
        await page.goto(invitedEditor);
        await page.getByRole("button", { name: "Publish version 2" }).click();
        await expect(page.getByText("Published version 2")).toBeVisible();
      }
      const invitee = uniqueEmail("invitee");
      await page.goto(invitedEditor);
      await page.locator('textarea[name="emails"]').fill(invitee);
      await page.getByRole("button", { name: "Add participants" }).click();
      await page.getByRole("button", { name: "Send 1 invite" }).click();
      const invite = await latestLinkTo(invitee, /https?:\/\/[^\s]+\/p\/[\w-]+/);
      for (const reader of ["en", "es"]) {
        const { context, page: respondent } = await visitor(browser, reader);
        await respondent.goto(invite);
        if (reader === "en") {
          await respondent.getByLabel("What went well?").fill("We shipped on time.");
          await minFillWait(respondent);
          await respondent.getByRole("button", { name: "Submit answers" }).click();
          await expect(respondent.locator(".js-answer-summary")).toBeVisible({ timeout: submitTimeout });
          await respondent.goto(invite);
        }
        await capture(respondent, name, reader);
        await context.close();
      }
    }
  }

  // The theme sheet: every component of a respondent's page in each
  // theme, which is where a theme's colours are all seen and scanned
  // together. It is served in development only, so there is none to
  // picture against another base URL.
  if (!process.env.E2E_BASE_URL) {
    for (const theme of ["earful", "slate", "ocean", "forest"]) {
      await page.goto("/dev/theme-sheet?theme=" + theme);
      await capture(page, `theme-sheet-${theme}`, "en");
    }
    await page.goto("/dev/theme-sheet?theme=ocean");
    await captureForced(page, "theme-sheet-ocean", "en");
  }

  // The creator in Spanish.
  const { context: spanish, page: es } = await visitor(browser, "es");
  await spanish.addCookies(await creatorContext.cookies());
  await es.goto("/dashboard");
  await capture(es, "dashboard", "es");
  await es.goto("/surveys/new");
  await capture(es, "survey-new", "es");
  await es.goto(editor);
  await capture(es, "editor-published", "es");
  await es.goto(thanksEditor);
  await capture(es, "editor-thanks", "es");
  await es.goto(otherEditor);
  await es.locator(".js-question summary").first().click();
  await capture(es, "editor-other", "es");
  await es.goto(otherEditor + "/results");
  await capture(es, "results-other", "es");
  await es.goto(editor + "/preview");
  await capture(es, "preview", "es");
  await es.goto(editor + "/preview?layout=all");
  await capture(es, "preview-all", "es");
  await es.goto(editor + "/results");
  await capture(es, "results", "es");
  await es.goto(editor + "/stats");
  await capture(es, "stats", "es");
  await es.goto(styledEditor + "/style");
  await capture(es, "style", "es");
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
