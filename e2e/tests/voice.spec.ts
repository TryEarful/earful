import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import {
  aiTimeout,
  createPublishedSurvey,
  fakeMicrophone,
  noMicrophone,
  offersVoice,
  scriptedVoice,
  slowSockets,
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
    await expect(respondent.getByRole("button", { name: "Dictate" })).toHaveCount(0);
    await respondent.locator("textarea").fill("Typed, because this instance has no mic.");
    await expect(respondent.locator("textarea")).not.toBeEmpty();
    await context.close();
  }
  test.skip(!offered, "this instance has no transcription configured, so it offers no mic");

  // The mic is offered on the long-text question, next to a textarea
  // that already works.
  const mic = respondent.getByRole("button", { name: "Dictate" });
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

  // The field itself says something is happening, not only the status
  // line beside the button — the respondent is looking at the box their
  // words are about to appear in.
  await expect(respondent.locator("textarea")).toHaveAttribute(
    "placeholder",
    /Recording in progress/
  );

  // While recording, the page shows what the microphone is hearing and
  // which microphone it is: the browser picks the input silently, and a
  // wrong pick otherwise surfaces only as an empty transcript.
  await expect(respondent.locator(".voice-monitor")).toBeVisible();
  await expect(respondent.locator(".voice-spectrum")).toBeVisible();

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

  // Once the take has settled the field is a plain textarea again: the
  // recording hint is gone rather than left behind as a stale caption,
  // and the transcription indicator is hidden. The indicator's visible
  // moment is deliberately not asserted — it lasts exactly as long as
  // the provider takes, and a race is not something a promotion gate can
  // afford. The microphone row stays, since its picker is a choice for
  // the next take.
  await expect(answer).not.toHaveAttribute("placeholder", /Recording in progress/);
  await expect(respondent.locator(".voice-progress")).toBeHidden();
  await expect(respondent.locator(".voice").first()).toHaveAttribute("data-state", "idle");
  await expect(respondent.getByLabel("Microphone")).toBeVisible();

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

// A microphone that cannot be opened is the commonest failure a
// respondent will meet — permission refused, no device — and the answer
// to it is typing. The controls grey out and the message is boxed as an
// error, and nothing about the keyboard changes: Space types a space.
test("without a microphone the voice controls are disabled and the error is boxed", async ({
  page,
  browser,
}) => {
  const share = await createPublishedSurvey(page, `E2E voice nomic ${Date.now()}`);

  const context = await browser.newContext({ storageState: undefined });
  await noMicrophone(context);
  const respondent = await context.newPage();
  await respondent.goto(share);

  const offered = await offersVoice(respondent);
  test.skip(!offered, "this instance has no transcription configured, so it offers no mic");

  await respondent.evaluate(() => localStorage.setItem("earful-voice-consent", "yes"));
  await respondent.reload();

  const mic = respondent.getByRole("button", { name: "Dictate" });
  const reset = respondent.getByRole("button", { name: "Reset", exact: true });
  const status = respondent.locator(".voice-status").first();
  await mic.click();

  await expect(status).toHaveText(/Microphone unavailable/);
  await expect(status).toHaveClass(/voice-error/);
  await expect(mic).toBeDisabled();
  await expect(reset).toBeDisabled();
  await expect(respondent.locator(".voice").first()).toHaveAttribute("data-state", "unavailable");

  // Typing is untouched: a tap of Space is the browser's own space, and
  // Esc twice clears nothing, because the keys no longer claim them.
  const answer = respondent.locator("textarea");
  await answer.click();
  await respondent.keyboard.type("typed instead, then");
  await respondent.keyboard.press("Escape");
  await respondent.keyboard.press("Escape");
  await expect(answer).toHaveValue("typed instead, then");
  await expect(status).toHaveText(/Microphone unavailable/);

  const scan = await new AxeBuilder({ page: respondent }).analyze();
  expect(scan.violations).toEqual([]);

  await context.close();
});

// The browser picks the input silently. Once a take has opened the
// microphone, the page lists the inputs it may use, marks the one in
// use, and lets the respondent change it — for the next take, since a
// stream cannot be swapped under a running capture graph. The choice is
// remembered per browser.
test("the microphone can be changed from a dropdown, and the choice sticks", async ({
  page,
  browser,
}) => {
  test.skip(
    !scriptedVoice,
    "refusing to send synthesized audio to a real transcriber (E2E_VOICE_MODE is not scripted)"
  );

  const share = await createPublishedSurvey(page, `E2E voice device ${Date.now()}`);

  const context = await browser.newContext({
    storageState: undefined,
    permissions: ["microphone"],
  });
  await fakeMicrophone(context);
  const respondent = await context.newPage();
  await respondent.goto(share);

  const offered = await offersVoice(respondent);
  test.skip(!offered, "this instance has no transcription configured, so it offers no mic");

  await respondent.evaluate(() => localStorage.setItem("earful-voice-consent", "yes"));
  await respondent.reload();

  const mic = respondent.getByRole("button", { name: "Dictate" });
  const stop = respondent.getByRole("button", { name: "Stop and transcribe" });
  const picker = respondent.getByLabel("Microphone");
  const status = respondent.locator(".voice-status").first();
  const lastRequest = () =>
    respondent.evaluate(() => {
      const requests = (window as any).__earfulMicRequests;
      return requests[requests.length - 1];
    });

  // Hidden until a take reveals the device names; then the browser's
  // default is the one marked, and the first request asked for no
  // device in particular.
  await expect(picker).toBeHidden();
  await mic.click();
  await expect(stop).toBeVisible();
  await expect(picker).toBeVisible();
  await expect(picker).toHaveValue("default");
  expect((await lastRequest()).audio.deviceId).toBeUndefined();

  // Changing mid-take ends the take; the next take is on the new device.
  await picker.selectOption("usb-1");
  await expect(mic).toBeVisible();
  await expect(status).toHaveText(/Transcribed/, { timeout: aiTimeout });

  await mic.click();
  await expect(stop).toBeVisible();
  expect((await lastRequest()).audio.deviceId).toEqual({ exact: "usb-1" });
  await expect(picker).toHaveValue("usb-1");
  await stop.click();
  await expect(status).toHaveText(/Transcribed/, { timeout: aiTimeout });

  // Remembered across a reload, and still a plain <select> with a name.
  await respondent.reload();
  await respondent.getByRole("button", { name: "Dictate" }).click();
  await expect(respondent.getByLabel("Microphone")).toHaveValue("usb-1");
  expect((await lastRequest()).audio.deviceId).toEqual({ exact: "usb-1" });
  const scan = await new AxeBuilder({ page: respondent }).analyze();
  expect(scan.violations).toEqual([]);

  await context.close();
});

// A take is a conversation that opens with start. On a slow connection
// the socket is still connecting when the microphone is already open,
// and a stop sent in that gap used to reach the server ahead of the
// start: the session never began, nothing was transcribed, and the page
// waited on "Transcribing…" for good. The stop control now appears only
// once the connection is open, so what is said is what is sent.
test("a take on a slow connection is still transcribed", async ({ page, browser }) => {
  test.skip(
    !scriptedVoice,
    "refusing to send synthesized audio to a real transcriber (E2E_VOICE_MODE is not scripted)"
  );

  const share = await createPublishedSurvey(page, `E2E voice slow ${Date.now()}`);

  const context = await browser.newContext({
    storageState: undefined,
    permissions: ["microphone"],
  });
  await fakeMicrophone(context);
  await slowSockets(context, 800);
  const respondent = await context.newPage();
  await respondent.goto(share);

  const offered = await offersVoice(respondent);
  test.skip(!offered, "this instance has no transcription configured, so it offers no mic");

  await respondent.evaluate(() => localStorage.setItem("earful-voice-consent", "yes"));
  await respondent.reload();

  const mic = respondent.getByRole("button", { name: "Dictate" });
  const stop = respondent.getByRole("button", { name: "Stop and transcribe" });
  const status = respondent.locator(".voice-status").first();

  await mic.click();
  await expect(stop).toBeVisible();
  // Long enough to have said something, far shorter than the handshake.
  await respondent.waitForTimeout(250);
  await stop.click();
  await expect(status).toHaveText(/Transcribed/, { timeout: aiTimeout });
  await expect(respondent.locator("textarea")).not.toBeEmpty();

  await context.close();
});
