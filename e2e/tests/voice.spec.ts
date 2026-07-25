import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import {
  aiTimeout,
  createPublishedSurvey,
  fakeMicrophone,
  offersVoice,
  scriptedVoice,
} from "./helpers";

// Spoken answers, in a real browser with a fake microphone (M5).
//
// The compose stack runs the scripted transcription provider by default,
// so this exercises the whole path — consent, capture, WebSocket, the
// transcript arriving in the textarea — without a model. The chunks the
// fake device produces are real audio frames as far as the page and the
// server are concerned.
//
// The fake capture device is a browser launch flag and lives in
// playwright.config.ts (launch options are worker-scoped); the microphone
// permission is per-context and lives here. Nothing about the app is
// special-cased for the test.
test.use({ permissions: ["microphone"] });

test("a spoken answer becomes an editable transcript", async ({ page, browser }) => {
  // Never against a real transcriber. The suite has no microphone, so
  // whatever it sends is machine-generated — and a loop of synthesized
  // audio arriving at a speech model is both useless as a test and the
  // likeliest thing to get a project suspended, which is exactly what
  // happened to staging on 2026-07-25. Real transcription is proven by
  // internal/ai's opt-in integration test, which sends real recorded
  // speech, once, deliberately.
  test.skip(
    !scriptedVoice,
    "refusing to send synthesized audio to a real transcriber (E2E_VOICE_MODE is not scripted)"
  );

  const share = await createPublishedSurvey(page, `E2E voice ${Date.now()}`);

  const context = await browser.newContext({
    storageState: undefined,
    permissions: ["microphone"],
  });
  await fakeMicrophone(context);
  const respondent = await context.newPage();
  await respondent.goto(share);

  const offered = await offersVoice(respondent);
  if (!offered) {
    // No transcription configured means no mic — and a typed answer
    // that works exactly as it always did. That is the whole promise
    // when the capability is absent.
    await expect(respondent.getByRole("button", { name: "Answer by speaking" })).toHaveCount(0);
    await respondent.locator("textarea").fill("Typed, because this instance has no mic.");
    await expect(respondent.locator("textarea")).not.toBeEmpty();
    await context.close();
  }
  test.skip(!offered, "this instance has no transcription configured, so it offers no mic");

  // The mic is offered on the long-text question, next to a textarea
  // that already works.
  const mic = respondent.getByRole("button", { name: "Answer by speaking" });
  await expect(mic).toBeVisible();
  await expect(respondent.locator("textarea")).toBeVisible();

  // First use asks for consent, and the promise is in the copy.
  await mic.click();
  const consent = respondent.getByRole("dialog");
  await expect(consent).toBeVisible();
  await expect(consent).toContainText("never stored");

  // The consent dialog itself must be accessible: it is the one piece of
  // UI a respondent cannot skip past.
  const consentScan = await new AxeBuilder({ page: respondent })
    .include(".voice-consent")
    .analyze();
  expect(consentScan.violations).toEqual([]);

  await respondent.getByRole("button", { name: "Use the microphone" }).click();

  // Recording starts; the button says how to end it.
  const stop = respondent.getByRole("button", { name: "Stop and transcribe" });
  await expect(stop).toBeVisible();
  await respondent.waitForTimeout(1500); // a second of speech to transcribe
  await stop.click();

  // Canned transcription, always (see the skip at the top): the words
  // are deterministic, so the whole promise is checkable — the
  // transcript lands in the textarea, where it can be edited before
  // submitting (story 36).
  const answer = respondent.locator("textarea");
  await expect(answer).not.toBeEmpty({ timeout: aiTimeout });
  const transcript = await answer.inputValue();
  expect(transcript.length).toBeGreaterThan(0);
  await answer.fill(transcript + " — edited before submitting.");
  await expect(answer).toHaveValue(/edited before submitting/);

  await context.close();
});

// The locality rule (M5-T1) checked against stubs rather than against six
// real browsers: the detector must refuse anything that cannot prove
// recognition happens on the device (ADR-0004).
test("local recognition is only claimed when the browser proves it", async ({ page }) => {
  const share = await createPublishedSurvey(page, `E2E voice detect ${Date.now()}`);
  await page.goto(share);

  // voice.js — and with it the detector under test — is only loaded when
  // the page offers a mic at all.
  const offered = await offersVoice(page);
  test.skip(!offered, "this instance has no transcription configured, so voice.js is not loaded");

  const results = await page.evaluate(() => {
    const detect = (window as any).EarfulVoice.detectLocalRecognition;
    class NoLocality {}
    class WithLocality {
      static available() {
        return "available";
      }
    }
    (WithLocality.prototype as any).processLocally = true;
    return {
      noApi: detect({}),
      // The classic Web Speech API: works, but streams audio to a vendor.
      classic: detect({ SpeechRecognition: NoLocality }),
      webkitClassic: detect({ webkitSpeechRecognition: NoLocality }),
      onDevice: detect({ SpeechRecognition: WithLocality }),
    };
  });

  expect(results.noApi.available).toBe(false);
  expect(results.classic.available).toBe(false);
  expect(results.classic.reason).toBe("no-locality-guarantee");
  expect(results.webkitClassic.available).toBe(false);
  expect(results.onDevice.available).toBe(true);
});
