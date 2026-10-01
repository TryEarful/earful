import { test, expect } from "@playwright/test";
import { latestLinkTo, signIn, uniqueEmail } from "./helpers";

// Closing an account with a copy of the data asked for: the copy
// arrives by email, and its link downloads the archive with no session.
// It needs an account of its own, since it closes the account it uses.
test.use({ storageState: { cookies: [], origins: [] } });

test("closing an account emails a copy that downloads without signing in", async ({ page, browser }) => {
  // Where mail is read back from Cloud Logging, each email waits on log
  // ingestion, and this test reads two of them (the sign in link, then the
  // copy, which is sent once its archive is built).
  if (process.env.E2E_LINK_SOURCE === "logging") test.slow();
  const addr = uniqueEmail("e2e-closure");
  await signIn(page, addr);

  await page.goto("/account");
  const sendCopy = page.locator(".js-delete-send-copy");
  await expect(sendCopy).not.toBeChecked();
  await sendCopy.check();
  await page.getByRole("button", { name: "Delete my account" }).click();

  await expect(page).toHaveURL(/\/goodbye\?copy=sent$/);
  await expect(page.locator(".js-goodbye-copy")).toBeVisible();

  const link = await latestLinkTo(addr, /https?:\/\/[^\s]+\/exports\/closure\/[\w-]+/);

  // A fresh context: the link is the only credential.
  const stranger = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const response = await stranger.request.get(link);
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toBe("application/zip");
  expect(response.headers()["cache-control"]).toBe("no-store");
  expect((await response.body()).length).toBeGreaterThan(0);

  const wrong = await stranger.request.get(link.replace(/[\w-]+$/, "not-a-token"));
  expect(wrong.status()).toBe(404);
  await stranger.close();
});
