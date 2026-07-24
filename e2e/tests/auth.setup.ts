import { test as setup } from "@playwright/test";
import { signIn, uniqueEmail } from "./helpers";

// One sign-in for the whole suite, saved as storage state that every
// project reuses. This is not (only) an optimization: magic-link requests
// are rate-limited per IP — the product behaving as designed — so a suite
// that signed in per test × per viewport would rate-limit itself mid-run.
setup("authenticate", async ({ page }) => {
  await signIn(page, uniqueEmail("e2e-shared-creator"));
  await page.context().storageState({ path: ".auth/creator.json" });
});
