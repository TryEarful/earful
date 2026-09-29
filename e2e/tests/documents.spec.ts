import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

// The documents in web/pages: the trust page, help. They are public, so
// every test here is a stranger in a fresh context.

test("a document can be copied as Markdown", async ({ browser }) => {
  const context = await browser.newContext({
    storageState: undefined,
    permissions: ["clipboard-read", "clipboard-write"],
  });
  const page = await context.newPage();
  await page.goto("/trust");
  await expect(page.getByRole("heading", { level: 1, name: "How Earful treats your data" })).toBeVisible();

  // Drawn hidden, shown by the script that makes it work.
  const copy = page.getByRole("button", { name: "Copy as Markdown" });
  await expect(copy).toBeVisible();
  await copy.click();
  await expect(page.getByRole("status")).toHaveText("Copied.");

  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied).toContain("# How Earful treats your data");
  expect(copied).toContain("## Your voice is never stored");
  expect(copied).toMatch(/^last_update: \d{4}-\d{2}-\d{2}$/m);
  // What is copied is the document, with this instance's facts in it.
  expect(copied).not.toContain("{{");
  await context.close();
});

test("a document is axe-clean", async ({ browser }) => {
  const context = await browser.newContext({ storageState: undefined });
  const page = await context.newPage();
  for (const address of ["/trust", "/help/voice"]) {
    await page.goto(address);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    const results = await new AxeBuilder({ page }).analyze();
    expect(results.violations, address).toEqual([]);
  }
  await context.close();
});

// The no-JS contract (story 29): without a script there is no button,
// and the link beside it serves the same Markdown.
test("a document's Markdown is a link away with JavaScript disabled", async ({ browser }) => {
  const context = await browser.newContext({ storageState: undefined, javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto("/help/voice");
  await expect(page.getByRole("heading", { level: 1, name: "Answering by voice" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Copy as Markdown" })).toBeHidden();

  const link = page.getByRole("link", { name: "View as Markdown" });
  await expect(link).toHaveAttribute("href", "/help/voice.md");
  const response = await context.request.get("/help/voice.md");
  expect(response.headers()["content-type"]).toContain("text/markdown");
  expect(await response.text()).toContain("# Answering by voice");
  await context.close();
});
