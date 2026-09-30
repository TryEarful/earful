// Playwright plumbing shared by the build and screenshot stages: contexts
// at the two widths, the magic-link sign-in, a fake microphone, and the
// respondent wizard.
//
// The sign-in and microphone are ports of e2e/tests/helpers.ts; the suite
// and this tool drive the same app, so the same care applies.

import { encodeBase64 } from "@std/encoding/base64";
import { join } from "@std/path";
import { type Browser, type BrowserContext, chromium, devices, type Page } from "playwright";
import { BASE, HEADED, REPO_ROOT } from "./config.ts";
import { seenMessages, waitForLink } from "./mailpit.ts";

export const DESKTOP = {
    viewport: { width: 1360, height: 860 },
    deviceScaleFactor: 2,
    colorScheme: "light" as const,
    reducedMotion: "reduce" as const,
};

export const PHONE = {
    ...devices["Pixel 7"],
    deviceScaleFactor: 2,
    colorScheme: "light" as const,
    reducedMotion: "reduce" as const,
};

export async function launch(): Promise<Browser> {
    return await chromium.launch({
        headless: !HEADED,
        // Accepts the microphone prompt should a page ever reach the real
        // getUserMedia; the fake below normally answers first.
        args: ["--use-fake-ui-for-media-stream"],
    });
}

/**
 * A capture device that plays testdata/jfk.wav as a MediaStream. No real
 * microphone is opened, and nothing synthesised is ever sent to a speech
 * model: with the mock this is transcribed to a fixed sentence, and with a
 * real model the recording is genuine speech.
 */
export async function fakeMicrophone(context: BrowserContext) {
    const wav = await Deno.readFile(join(REPO_ROOT, "testdata", "jfk.wav"));
    const encoded = encodeBase64(wav);
    await context.addInitScript((b64: string) => {
        const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
        navigator.mediaDevices.getUserMedia = async () => {
            const audio = new AudioContext();
            await audio.resume();
            const buffer = await audio.decodeAudioData(bytes.buffer.slice(0) as ArrayBuffer);
            const source = audio.createBufferSource();
            const sink = audio.createMediaStreamDestination();
            source.buffer = buffer;
            source.loop = true;
            source.connect(sink);
            source.start();
            return sink.stream;
        };
    }, encoded);
}

export const MAGIC_LINK = /https?:\/\/[^\s]+\/auth\/magic\/verify\?token=[\w-]+/;
export const INVITE_LINK = /https?:\/\/[^\s]+\/p\/[\w-]+/;

export interface SignInHooks {
    /** After the request, before the link is opened. */
    afterRequest?: (page: Page) => Promise<void>;
    /** With the message id, before the link is opened. */
    onMessage?: (page: Page, messageId: string) => Promise<void>;
    /** On the confirmation page, before the button is pressed. */
    beforeConfirm?: (page: Page) => Promise<void>;
}

/** The full magic-link flow: request, read the inbox, confirm. */
export async function signIn(page: Page, email: string, hooks: SignInHooks = {}) {
    const before = await seenMessages(email);
    await page.goto(BASE + "/login");
    await page.getByLabel("Email address").fill(email);
    await page.getByRole("button", { name: "Email me a link to sign in" }).click();
    await page.getByRole("heading", { name: "Check your email" }).waitFor();
    await hooks.afterRequest?.(page);
    const found = await waitForLink(email, MAGIC_LINK, before);
    await hooks.onMessage?.(page, found.id);
    await page.goto(found.link);
    await hooks.beforeConfirm?.(page);
    await page.getByRole("button", { name: "Sign in" }).click();
    await page.waitForURL(/\/dashboard$/);
}

/** Outlasts the server's minimum-fill-time check on the respondent form. */
export async function minFillWait(page: Page) {
    await page.waitForTimeout(5_000);
}

/**
 * Answers every question of the respondent form with plausible values.
 * The honeypot (input[name=website]) is left alone: filling it makes the
 * server fake a success and store nothing.
 */
export async function fillRespondForm(page: Page, text: string) {
    await page.evaluate((text: string) => {
        const seen = new Set<string>();
        document.querySelectorAll<HTMLInputElement>("input[type=radio]").forEach((r) => {
            if (seen.has(r.name)) return;
            seen.add(r.name);
            const all = [
                ...document.querySelectorAll<HTMLInputElement>(
                    `input[type=radio][name="${r.name}"]`,
                ),
            ];
            const yes = all.find((x) => x.value === "yes");
            (yes ?? all[Math.max(0, all.length - 2)]).checked = true;
        });
        document.querySelectorAll<HTMLTextAreaElement>("textarea").forEach((t) => {
            if (!t.value) t.value = text;
        });
        document.querySelectorAll<HTMLInputElement>("input[type=text]").forEach((t) => {
            if (t.name === "website" || t.name === "lang") return;
            if (!t.value) t.value = "Nothing comes to mind.";
        });
        document.querySelectorAll<HTMLInputElement>("input[type=checkbox]").forEach((c, i) => {
            if (i % 2 === 0) c.checked = true;
        });
        document.querySelectorAll<HTMLSelectElement>("select").forEach((s) => {
            if (s.name !== "lang") s.selectedIndex = Math.min(2, s.options.length - 1);
        });
    }, text);
}

/** The respondent page shows one question at a time; walks to the submit button. */
export async function nextUntilSubmit(page: Page, onStep?: (i: number) => Promise<void>) {
    for (let i = 0; i < 40; i++) {
        const submit = page.getByRole("button", { name: "Submit answers" });
        if (await submit.isVisible().catch(() => false)) return;
        await onStep?.(i);
        await page.getByRole("button", { name: "Next" }).click();
        await page.waitForTimeout(250);
    }
    throw new Error("respondent wizard: submit button never appeared");
}

/** Walks the wizard until the question with this text is the visible step. */
export async function stepTo(page: Page, questionText: string) {
    for (let i = 0; i < 40; i++) {
        const q = page.locator(".js-respond-question:visible", { hasText: questionText });
        if (await q.count()) return q.first();
        await page.getByRole("button", { name: "Next" }).click();
        await page.waitForTimeout(250);
    }
    throw new Error(`respondent wizard: never reached "${questionText}"`);
}

/** The visible step of the wizard, for a focused screenshot. */
export function visibleStep(page: Page) {
    return page.locator(".js-respond-question:visible").first();
}
