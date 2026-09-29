import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createPublishedSurvey, minFillWait, offersVoice, submitTimeout } from "./helpers";

// The interface in another language (ADR-0014). The rest of the suite
// runs in English and finds things by what they say; this is the same
// journey taken by somebody whose browser asks for Spanish.

test("a respondent with a Spanish browser answers in Spanish", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E español ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined, locale: "es-ES" });
  const respondent = await context.newPage();
  await respondent.goto(share);

  // The page, and what the script draws on it: the position, the
  // buttons, the keys they name.
  await expect(respondent.locator("html")).toHaveAttribute("lang", "es");
  await expect(respondent.locator(".respond-progress")).toHaveText("Pregunta 1 de 2");
  await expect(respondent.getByRole("button", { name: "Siguiente" })).toBeVisible();
  await expect(respondent.getByRole("button", { name: "Siguiente" }).locator(".key-hint")).toContainText(["↵ Intro"]);
  await expect(respondent.getByText("Es anónima")).toBeVisible();

  const results = await new AxeBuilder({ page: respondent }).analyze();
  expect(results.violations).toEqual([]);

  await respondent.locator("textarea").fill("Déjenme hablar en vez de escribir.");
  await respondent.getByRole("button", { name: "Siguiente" }).click();
  await expect(respondent.locator(".respond-progress")).toHaveText("Pregunta 2 de 2");
  await expect(respondent.getByRole("button", { name: "Atrás" })).toBeVisible();
  // What the creator wrote is the creator's, in whatever language.
  await respondent.getByLabel("Monthly").check();

  await minFillWait(respondent);
  await respondent.getByRole("button", { name: "Enviar respuestas" }).click();
  await expect(respondent.getByRole("heading", { name: "Gracias" })).toBeVisible({ timeout: submitTimeout });

  // Nothing about the language was kept.
  const cookies = await context.cookies();
  expect(cookies.filter((c) => c.name.toLowerCase().includes("lang"))).toEqual([]);
  await context.close();
});

test("dictation asks for the microphone in Spanish", async ({ page, browser }) => {
  const share = await createPublishedSurvey(page, `E2E dictado ${Date.now()}`);
  const context = await browser.newContext({ storageState: undefined, locale: "es-ES" });
  const respondent = await context.newPage();
  await respondent.goto(share);
  test.skip(!(await offersVoice(respondent)), "this instance offers no voice");

  await expect(respondent.getByText("Pulse Dictar", { exact: false })).toBeVisible();
  await respondent.getByRole("button", { name: "Dictar" }).click();
  const dialog = respondent.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "Dicte su respuesta" })).toBeVisible();
  await expect(dialog.getByText("Su voz nunca se guarda")).toBeVisible();
  await dialog.getByRole("button", { name: "Ahora no" }).click();
  await expect(dialog).toHaveCount(0);
  await context.close();
});

test("the switcher changes the language and remembers it", async ({ browser }) => {
  const context = await browser.newContext({ storageState: undefined, locale: "en-US" });
  const page = await context.newPage();
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();

  // Offered by the name the language gives itself, to somebody who may
  // not read the language the page is in.
  await page.getByLabel("Language").selectOption({ label: "Español" });
  await page.getByRole("button", { name: "Change", exact: true }).click();

  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "Iniciar sesión" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("lang", "es");

  // Remembered on the next page, by a browser that still asks for English.
  await page.goto("/trust");
  await expect(page.getByRole("heading", { level: 1, name: "Cómo trata Earful sus datos" })).toBeVisible();
  await page.goto("/login");
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);

  await page.getByLabel("Idioma").selectOption({ label: "English" });
  await page.getByRole("button", { name: "Cambiar", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await context.close();
});
