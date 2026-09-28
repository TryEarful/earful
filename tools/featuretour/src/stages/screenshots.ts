// screenshots: every capture that needs the seeded data, taken with the
// sessions the build stage saved. Ids are what content/deck.md refers to.
//
// Runs are idempotent: the AI panels serve their caches on a rerun, the
// closed survey is reopened, and files are overwritten in place.

import { join } from "@std/path";
import type { Browser, BrowserContext, Page } from "playwright";
import { BASE, RESULTS_DIR } from "../config.ts";
import type { StoryDef, Surveys } from "../content.ts";
import {
    DESKTOP,
    fakeMicrophone,
    fillRespondForm,
    minFillWait,
    nextUntilSubmit,
    PHONE,
    stepTo,
    visibleStep,
} from "../browser.ts";
import { type Manifest, shoot } from "../shots.ts";
import { latest, type State, type StoryState } from "../state.ts";

export async function screenshots(
    browser: Browser,
    surveys: Surveys,
    state: State,
    manifest: Manifest,
) {
    await Deno.mkdir(RESULTS_DIR, { recursive: true });
    await publicPages(browser, manifest);
    for (const [name, story] of Object.entries(surveys.stories)) {
        const st = state.stories[name];
        if (!st) throw new Error(`screenshots: no state for ${name}`);
        console.log(`screenshots: ${name} (creator)`);
        const ctx = await browser.newContext({ ...DESKTOP, storageState: st.storageState });
        const page = await ctx.newPage();
        const s = new Story(name, story, st, manifest, page, ctx, erasureSubject(state));
        await s.creator();
        await ctx.close();
        console.log(`screenshots: ${name} (respondent)`);
        await s.respondents(browser);
    }
    await phoneViews(browser, state, manifest);
}

class Story {
    constructor(
        private name: string,
        private def: StoryDef,
        private st: StoryState,
        private manifest: Manifest,
        private page: Page,
        private ctx: BrowserContext,
        /** An address the erasure page can look up, so the screenshot shows a result. */
        private subject: string,
    ) {}

    private shot(id: string, opts: Parameters<typeof shoot>[3]) {
        return shoot(this.manifest, this.page, `${id}`, opts);
    }

    private card(heading: string | RegExp) {
        return this.page.locator(".card", {
            has: this.page.locator("h2", {
                hasText: typeof heading === "string" ? new RegExp(`^${heading}$`) : heading,
            }),
        }).first();
    }

    private resultCard(key: string) {
        const q = latest(this.st).questions.find((q) => q.key === key)!;
        return this.page.locator(".card.result", {
            has: this.page.locator("h2", { hasText: q.text }),
        }).first();
    }

    async creator() {
        const { page, name, st } = this;
        const survey = `${BASE}/surveys/${st.surveyId}`;
        await page.goto(BASE + "/dashboard");
        await this.shot(`dashboard-${name}`, { kind: "viewport" });

        // Results, card by card.
        await page.goto(`${survey}/results`);
        await this.shot(`results-${name}`, { kind: "viewport" });
        for (const details of await page.locator(".wordings summary").all()) await details.click();
        for (const q of latest(st).questions) {
            await this.shot(`result-${name}-${q.key}`, {
                kind: "card",
                locator: this.resultCard(q.key),
            });
        }
        await page.locator("summary", { hasText: "All responses" }).click();
        await page.waitForTimeout(200);
        await this.shot(`results-${name}-table`, {
            kind: "viewport",
            locator: page.locator("details", { has: page.locator("table.responses") }),
        });

        // The Insight Summary, mid-stream and once labelled.
        const insights = page.locator("#insights");
        await page.goto(`${survey}/results`);
        await page.locator("#insights form[data-enhanced]").waitFor({ timeout: 10_000 }).catch(
            () => {},
        );
        await insights.getByRole("button", { name: /Analyse the responses|Run the analysis again/ })
            .click();
        await page.waitForFunction(
            () =>
                (document.querySelector("#insights .insight-output")?.textContent ?? "").length >
                    300,
            null,
            { timeout: 120_000 },
        ).catch(() => {});
        await this.shot(`insights-${name}-streaming`, { kind: "card", locator: insights });
        await page.locator("#insights .ai-label").waitFor({ timeout: 300_000 });
        await this.shot(`insights-${name}-done`, { kind: "card", locator: insights });

        // Answer translation, for stories with text answers.
        const textKey = this.def.questions.find((q) =>
            q.type === "long_text" || q.type === "short_text"
        )?.key;
        if (textKey) {
            await page.goto(`${survey}/results`);
            await this.shot(`translate-${name}`, {
                kind: "viewport",
                locator: page.locator(".results-actions"),
            });
            await page.locator(".results-actions input[name=lang]").fill("en");
            await page.getByRole("button", { name: "Translate" }).click();
            await page.waitForURL(/lang=en/, { timeout: 300_000 });
            await page.waitForLoadState("networkidle");
            await this.shot(`result-${name}-${textKey}-translated`, {
                kind: "card",
                locator: this.resultCard(textKey),
            });
        }

        // Stats.
        await page.goto(`${survey}/stats`);
        await this.shot(`stats-${name}`, { kind: "viewport" });
        await this.shot(`stats-${name}-big-picture`, {
            kind: "card",
            locator: this.card("Big picture"),
        });
        await this.shot(`stats-${name}-trend`, { kind: "card", locator: page.locator("#trend") });
        await this.shot(`stats-${name}-questions`, {
            kind: "card",
            locator: page.locator("#questions"),
        });
        await this.shot(`stats-${name}-audience`, {
            kind: "card",
            locator: page.locator(".card.stats"),
        });

        // The CSVs the pages offer.
        await save(page, `${survey}/results.csv`, join(RESULTS_DIR, `${name}.csv`));
        await save(page, `${survey}/stats.csv`, join(RESULTS_DIR, `${name}-stats.csv`));

        if (this.def.anonymity === "invited") {
            await page.goto(survey);
            await this.shot(`participants-${name}`, {
                kind: "card",
                locator: this.card("Participants"),
            });
        }

        if (this.def.super_admin) {
            await this.operator();
        }

        if (name === "restaurant") {
            await this.closeAndReopen();
        }
    }

    /** Account export and the admin pages, from the operator's account. */
    private async operator() {
        const { page } = this;
        await page.goto(BASE + "/account");
        await this.shot("account", { kind: "viewport" });
        await page.locator('form[action="/account/export"] button').click();
        for (let i = 0; i < 60; i++) {
            if (
                await page.getByRole("link", { name: "Download the archive" }).isVisible().catch(
                    () => false,
                )
            ) break;
            await page.waitForTimeout(1000);
            await page.goto(BASE + "/account");
        }
        await this.shot("export-ready", { kind: "card", locator: this.card("Export everything") });
        const href = await page.getByRole("link", { name: "Download the archive" }).getAttribute(
            "href",
        );
        if (href) {
            await save(
                page,
                new URL(href, BASE).toString(),
                join(RESULTS_DIR, "workspace-export.zip"),
            );
        }

        await page.goto(BASE + "/admin/metrics");
        await this.shot("admin-metrics", { kind: "viewport" });
        await page.goto(BASE + "/admin/erasure");
        if (this.subject) {
            await page.locator("input[name=email]").fill(this.subject);
            await page.getByRole("button", { name: "Look up" }).click();
            await page.waitForLoadState("networkidle");
        }
        await this.shot("admin-erasure", { kind: "viewport" });
        await page.goto(BASE + "/admin/beta-codes");
        await this.shot("admin-beta-codes", { kind: "viewport" });
    }

    private async closeAndReopen() {
        const { page, st, ctx } = this;
        await page.goto(`${BASE}/surveys/${st.surveyId}`);
        await page.getByRole("button", { name: "Close survey" }).click();
        await page.waitForLoadState("networkidle");
        await this.shot("closed-editor", { kind: "card", locator: this.card("Sharing") });
        const visitor = await ctx.browser()!.newContext(DESKTOP);
        const vp = await visitor.newPage();
        await vp.goto(st.shareUrl);
        await shoot(this.manifest, vp, "closed-respondent", { kind: "viewport" });
        await visitor.close();
        await page.getByRole("button", { name: "Reopen survey" }).click();
        await page.waitForLoadState("networkidle");
    }

    /** Fresh, signed-out contexts: what a respondent sees. */
    async respondents(browser: Browser) {
        const { name, st, def, manifest } = this;
        if (def.anonymity === "invited") {
            await this.invited(browser);
            return;
        }
        const ctx = await browser.newContext(DESKTOP);
        const page = await ctx.newPage();
        await page.goto(st.shareUrl);
        await shoot(manifest, page, `respond-${name}`, { kind: "viewport" });
        for (const lang of def.languages ?? []) {
            await page.goto(`${st.shareUrl}?lang=${lang}`);
            await shoot(manifest, page, `respond-${name}-${lang}`, { kind: "viewport" });
        }
        await page.goto(st.shareUrl);
        await fillRespondForm(page, def.respondent_answer);
        await minFillWait(page);
        const keys = latest(st).questions.map((q) => q.key);
        await nextUntilSubmit(page, async (i) => {
            if (keys[i]) {
                await shoot(manifest, page, `respond-${name}-${keys[i]}`, {
                    kind: "viewport",
                    locator: visibleStep(page),
                });
            }
        });
        await page.getByRole("button", { name: "Submit answers" }).click();
        await page.getByRole("heading", { name: "Thank you" }).waitFor({ timeout: 20_000 });
        await shoot(manifest, page, `respond-${name}-thanks`, { kind: "viewport" });
        await ctx.close();

        const phone = await browser.newContext(PHONE);
        const pp = await phone.newPage();
        await pp.goto(st.shareUrl);
        await shoot(manifest, pp, `respond-${name}-phone`, { kind: "phone" });
        await phone.close();

        if (name === "earful") await this.voice(browser);
    }

    private async invited(browser: Browser) {
        const { st, def, manifest, name } = this;
        const submitted = new Set(def.submitted ?? []);
        const fresh = st.participants.find((p) => !submitted.has(p.email) && p.inviteUrl);
        const done = st.participants.find((p) => submitted.has(p.email) && p.inviteUrl);
        const ctx = await browser.newContext(DESKTOP);
        const page = await ctx.newPage();
        if (fresh) {
            await page.goto(fresh.inviteUrl!);
            await shoot(manifest, page, `respond-${name}`, { kind: "viewport" });
        }
        if (done) {
            await page.goto(done.inviteUrl!);
            await shoot(manifest, page, `respond-${name}-already`, { kind: "viewport" });
        }
        await ctx.close();
    }

    /** A spoken answer: consent, recording, transcript. */
    private async voice(browser: Browser) {
        const { st, def, manifest } = this;
        const spoken = def.questions.find((q) => q.type === "long_text");
        if (!spoken) return;
        const ctx = await browser.newContext({ ...DESKTOP, permissions: ["microphone"] });
        await fakeMicrophone(ctx);
        const page = await ctx.newPage();
        await page.goto(st.shareUrl);
        await fillRespondForm(page, "");
        const step = await stepTo(page, spoken.text);
        await step.getByRole("button", { name: "Answer by speaking" }).click();
        await page.getByRole("dialog").waitFor();
        await shoot(manifest, page, "voice-consent", {
            kind: "viewport",
            locator: page.getByRole("dialog"),
        });
        await page.getByRole("button", { name: "Use the microphone" }).click();
        await page.getByRole("button", { name: "Stop and transcribe" }).waitFor();
        await page.waitForTimeout(2500);
        await shoot(manifest, page, "voice-recording", { kind: "viewport", locator: step });
        await page.getByRole("button", { name: "Stop and transcribe" }).click();
        await page.waitForFunction(
            () => [...document.querySelectorAll("textarea")].some((t) => t.value.length > 20),
            null,
            { timeout: 120_000 },
        );
        await page.waitForTimeout(600);
        await shoot(manifest, page, "voice-transcript", { kind: "viewport", locator: step });
        await ctx.close();
    }
}

/** The pages anyone can open. */
async function publicPages(browser: Browser, manifest: Manifest) {
    console.log("screenshots: public pages");
    const ctx = await browser.newContext(DESKTOP);
    const page = await ctx.newPage();
    await page.goto(BASE + "/");
    await shoot(manifest, page, "home", { kind: "viewport", locator: page.locator("body") });
    await page.goto(BASE + "/trust");
    await shoot(manifest, page, "trust", { kind: "page" });
    await ctx.close();
}

/** The creator pages at phone width, from the first story's session. */
async function phoneViews(browser: Browser, state: State, manifest: Manifest) {
    const [name, st] = Object.entries(state.stories)[0];
    console.log(`screenshots: ${name} (phone)`);
    const ctx = await browser.newContext({ ...PHONE, storageState: st.storageState });
    const page = await ctx.newPage();
    await page.goto(BASE + "/dashboard");
    await shoot(manifest, page, "phone-dashboard", { kind: "phone" });
    await page.goto(`${BASE}/surveys/${st.surveyId}/results`);
    await shoot(manifest, page, "phone-results", { kind: "phone" });
    await page.goto(`${BASE}/surveys/${st.surveyId}/stats`);
    await shoot(manifest, page, "phone-stats", { kind: "phone" });
    await ctx.close();
}

async function save(page: Page, url: string, file: string) {
    const res = await page.request.get(url);
    if (!res.ok()) throw new Error(`${url}: ${res.status()}`);
    await Deno.writeFile(file, await res.body());
}

/** A participant of the invited story: the erasure page has something to list for them. */
function erasureSubject(state: State): string {
    for (const st of Object.values(state.stories)) {
        if (st.participants[0]) return st.participants[0].email;
    }
    return "";
}
