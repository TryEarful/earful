// build: three accounts, three surveys, made through the product the way
// a creator would, with the screenshots that only exist mid-flow.
//
// Everything the later stages need to find again (survey ids, question
// ids per version, invite links) is written to out/state.json.

import { join } from "@std/path";
import type { Browser, Page } from "playwright";
import { AUTH_DIR, BASE } from "../config.ts";
import { lit, psql, query, run } from "../compose.ts";
import type { AIContent, StoryDef, Surveys } from "../content.ts";
import { DESKTOP, INVITE_LINK, signIn } from "../browser.ts";
import { messageURL, seenMessages, waitForLink } from "../mailpit.ts";
import { type Manifest, shoot } from "../shots.ts";
import {
    type ParticipantRef,
    saveState,
    type State,
    type StoryState,
    type VersionRef,
} from "../state.ts";

export async function build(
    browser: Browser,
    surveys: Surveys,
    ai: AIContent,
    manifest: Manifest,
): Promise<State> {
    await assertClean(surveys);
    const state: State = { createdAt: new Date().toISOString(), baseUrl: BASE, stories: {} };
    for (const [name, story] of Object.entries(surveys.stories)) {
        console.log(`build: ${name}`);
        state.stories[name] = await buildStory(browser, name, story, ai, manifest);
        await saveState(state);
    }
    return state;
}

/** A rerun without reset would double every survey; refuse instead. */
async function assertClean(surveys: Surveys) {
    const emails = Object.values(surveys.stories).map((s) => lit(s.email)).join(", ");
    const rows = await query<{ n: number }>(`
    SELECT count(*)::int AS n FROM surveys s
      JOIN workspace_members m ON m.workspace_id = s.workspace_id
      JOIN users u ON u.id = m.user_id
     WHERE u.email IN (${emails}) AND u.deleted_at IS NULL AND s.deleted_at IS NULL`);
    if (rows[0]?.n > 0) {
        throw new Error("the demo accounts already hold surveys: run with --from reset");
    }
}

interface Account {
    userId: string;
    workspaceId: string;
}

async function buildStory(
    browser: Browser,
    name: string,
    story: StoryDef,
    ai: AIContent,
    manifest: Manifest,
): Promise<StoryState> {
    const context = await browser.newContext(DESKTOP);
    const page = await context.newPage();
    // The first story's sign-in is the one the deck shows.
    const first = name === "restaurant";
    await signIn(page, story.email, {
        afterRequest: first
            ? async (p) => await shoot(manifest, p, "login-requested", { kind: "viewport" })
            : undefined,
        onMessage: first
            ? async (p, id) => {
                const inbox = await context.newPage();
                await inbox.goto(messageURL(id));
                await inbox.waitForTimeout(1500);
                await shoot(manifest, inbox, "mailpit-magic-link", {
                    kind: "viewport",
                    locator: inbox.locator("body"),
                });
                await inbox.close();
                await p.goto(BASE + "/login");
                await shoot(manifest, p, "login", { kind: "viewport" });
            }
            : undefined,
        beforeConfirm: first
            ? async (p) => await shoot(manifest, p, "confirm-signin", { kind: "viewport" })
            : undefined,
    });
    const account = await claimAccount(story);
    await Deno.mkdir(AUTH_DIR, { recursive: true });
    const storageState = join(AUTH_DIR, name + ".json");
    await context.storageState({ path: storageState });
    if (first) {
        await giveStarterSurvey(story, account);
        await page.goto(BASE + "/dashboard");
        await shoot(manifest, page, "dashboard-first", { kind: "viewport" });
    }
    await putAwayStarterSurvey(account);

    // Create the survey.
    await page.goto(BASE + "/surveys/new");
    await page.getByLabel("Title").fill(story.title);
    await page.locator(`input[name=anonymity][value=${story.anonymity}]`).check();
    if (first) await shoot(manifest, page, "new-survey", { kind: "viewport" });
    await page.getByRole("button", { name: "Create survey" }).click();
    await page.getByRole("heading", { name: story.title }).waitFor();
    const surveyId = page.url().match(/\/surveys\/([0-9a-f-]{36})/)![1];
    if (first) await shoot(manifest, page, "editor-empty", { kind: "viewport" });

    // Draft the questions with AI.
    await draftQuestions(page, story, manifest, first);
    if (first) await shoot(manifest, page, "editor-questions", { kind: "page" });

    // Languages: drafted by the model, options filled from the dictionary, reviewed.
    for (const lang of story.languages ?? []) {
        await addLanguage(page, surveyId, lang, ai, manifest, first);
    }

    // Publish version 1.
    await publish(page, surveyId, 1);
    const shareHref = await page.locator(".share-link a").getAttribute("href");
    if (!shareHref) throw new Error(`${name}: no share link after publishing`);
    const shareUrl = new URL(shareHref, BASE).toString();
    if (first) {
        await shoot(manifest, page, "card-sharing", {
            kind: "card",
            locator: card(page, "Sharing"),
        });
        await shoot(manifest, page, "card-versions", {
            kind: "card",
            locator: card(page, "Published versions"),
        });
        await shoot(manifest, page, "card-settings", {
            kind: "card",
            locator: card(page, "Settings"),
        });
        await page.goto(`${BASE}/surveys/${surveyId}/preview`);
        await shoot(manifest, page, "preview", { kind: "viewport" });
    }

    // Participants and invitations.
    let participants: ParticipantRef[] = [];
    if (story.anonymity === "invited") {
        participants = await invite(page, surveyId, story, manifest, context);
    }

    // A reworded question, published as version 2.
    if (story.reword) {
        await reword(page, surveyId, story, manifest);
    }

    const versions = await readVersions(surveyId, story);
    await context.close();
    return {
        email: story.email,
        userId: account.userId,
        workspaceId: account.workspaceId,
        surveyId,
        shareUrl,
        storageState,
        versions,
        participants,
    };
}

/** Names the workspace after the story and grants the operator flag where asked. */
async function claimAccount(story: StoryDef): Promise<Account> {
    const rows = await query<{ user_id: string; workspace_id: string }>(`
    SELECT u.id AS user_id, m.workspace_id FROM users u
      JOIN workspace_members m ON m.user_id = u.id
     WHERE u.email = ${lit(story.email)} AND u.deleted_at IS NULL
     ORDER BY m.created_at LIMIT 1`);
    const account = rows[0];
    if (!account) throw new Error(`no workspace for ${story.email} after sign-in`);
    await psql(`
UPDATE workspaces SET name = ${lit(story.workspace)} WHERE id = ${lit(account.workspace_id)};
UPDATE users SET is_super_admin = ${story.super_admin ? "true" : "false"} WHERE id = ${
        lit(account.user_id)
    };`);
    return { userId: account.user_id, workspaceId: account.workspace_id };
}

/**
 * A workspace is created holding its Starter Survey, and an account from
 * an earlier run is not created again: reset deleted its surveys, that
 * one included. The first dashboard is to look the same either way, so
 * where the survey is missing it is added the way an operator would.
 */
async function giveStarterSurvey(story: StoryDef, account: Account) {
    const rows = await query<{ n: number }>(`
    SELECT count(*)::int AS n FROM surveys
     WHERE workspace_id = ${lit(account.workspaceId)}
       AND origin = 'starter' AND deleted_at IS NULL`);
    if (rows[0]?.n > 0) return;
    await run("docker", [
        "compose",
        "--profile",
        "app",
        "exec",
        "-T",
        "app",
        "/earful",
        "starter-survey",
        "add",
        story.email,
    ]);
}

/**
 * The stories are about the surveys they make. The Starter Survey is
 * shown once, on the first dashboard, and then deleted as its owner
 * might, so that every later page holds the story's survey alone.
 */
async function putAwayStarterSurvey(account: Account) {
    await psql(`
UPDATE surveys SET deleted_at = now()
 WHERE workspace_id = ${lit(account.workspaceId)}
   AND origin = 'starter' AND deleted_at IS NULL;`);
}

function card(page: Page, heading: string) {
    return page.locator(".card", {
        has: page.locator("h2", { hasText: new RegExp(`^${heading}$`) }),
    })
        .first();
}

async function draftQuestions(page: Page, story: StoryDef, manifest: Manifest, shots: boolean) {
    const panel = page.locator("#ai-generate");
    await panel.locator("textarea[name=prompt]").fill(story.prompt);
    // generate.js marks the form once it owns the submit; before that a click
    // would take the plain-POST path, which has no streaming to show.
    await page.locator("form.generate-form[data-enhanced]").waitFor({ timeout: 10_000 });
    if (shots) await shoot(manifest, page, "ai-draft-prompt", { kind: "card", locator: panel });
    await panel.getByRole("button", { name: "Draft questions" }).click();
    if (shots) {
        await page.waitForFunction(
            () =>
                (document.querySelector(".generate-output")?.textContent ?? "").split("\n")
                    .length >= 3,
            null,
            { timeout: 120_000 },
        );
        await shoot(manifest, page, "ai-draft-streaming", { kind: "card", locator: panel });
    }
    // The page reloads itself when the stream ends; every question is then in the list.
    await page.waitForFunction(
        (n: number) => document.querySelectorAll("li.question").length >= n,
        story.questions.length,
        { timeout: 180_000 },
    );
}

async function addLanguage(
    page: Page,
    surveyId: string,
    lang: string,
    ai: AIContent,
    manifest: Manifest,
    shots: boolean,
) {
    await page.goto(`${BASE}/surveys/${surveyId}/localizations`);
    if (shots) await shoot(manifest, page, "languages-empty", { kind: "viewport" });
    await page.locator("input[name=lang]").fill(lang);
    await page.getByRole("button", { name: "Add language" }).click();
    await page.getByRole("button", { name: "Draft the missing translations with AI" }).click();
    await page.getByText(/Drafted \d+ translations?/).waitFor({ timeout: 180_000 });
    const section = page.locator(`#lang-${lang}`);
    if (shots) await shoot(manifest, page, "languages-drafted", { kind: "card", locator: section });
    // The model drafts question text only; options are translated here from
    // the same dictionary, the way a reviewer would fill them in.
    const dictionary = ai.translations[lang] ?? {};
    for (const area of await section.locator("textarea[name^=o_]").all()) {
        const source = (await area.getAttribute("placeholder")) ?? "";
        await area.fill(source.split("\n").map((o) => dictionary[o] ?? o).join("\n"));
    }
    await section.getByRole("button", { name: "Save and mark reviewed" }).click();
    await page.getByText(/Saved and marked reviewed/).waitFor();
    if (shots) {
        await shoot(manifest, page, "languages-reviewed", {
            kind: "card",
            locator: page.locator(`#lang-${lang}`),
        });
    }
}

async function publish(page: Page, surveyId: string, n: number) {
    await page.goto(`${BASE}/surveys/${surveyId}`);
    await page.getByRole("button", { name: `Publish version ${n}` }).click();
    await page.getByText(`Published version ${n}`).waitFor();
}

async function invite(
    page: Page,
    surveyId: string,
    story: StoryDef,
    manifest: Manifest,
    context: Awaited<ReturnType<Browser["newContext"]>>,
): Promise<ParticipantRef[]> {
    const emails = story.participants ?? [];
    await page.goto(`${BASE}/surveys/${surveyId}`);
    await page.locator("textarea[name=emails]").fill(emails.join("\n"));
    await page.getByRole("button", { name: "Add participants" }).click();
    await page.waitForLoadState("networkidle");
    await shoot(manifest, page, "participants-added", {
        kind: "card",
        locator: card(page, "Participants"),
    });
    const before = new Map<string, Set<string>>();
    for (const email of emails) before.set(email, await seenMessages(email));
    await page.getByRole("button", { name: /Send \d+ invites?/ }).click();
    await page.waitForLoadState("networkidle");
    await shoot(manifest, page, "participants-sent", {
        kind: "card",
        locator: card(page, "Participants"),
    });

    const rows = await query<{ id: string; email: string }>(
        `SELECT id, email FROM participants WHERE survey_id = ${
            lit(surveyId)
        } AND deleted_at IS NULL`,
    );
    const participants: ParticipantRef[] = [];
    let shown = false;
    for (const email of emails) {
        const found = await waitForLink(email, INVITE_LINK, before.get(email));
        const row = rows.find((r) => r.email === email);
        if (!row) throw new Error(`participant ${email} was not stored`);
        participants.push({ email, id: row.id, inviteUrl: found.link });
        if (!shown) {
            const inbox = await context.newPage();
            await inbox.goto(messageURL(found.id));
            await inbox.waitForTimeout(1500);
            await shoot(manifest, inbox, "invite-email", {
                kind: "viewport",
                locator: inbox.locator("body"),
            });
            await inbox.close();
            shown = true;
        }
    }
    return participants;
}

async function reword(page: Page, surveyId: string, story: StoryDef, manifest: Manifest) {
    await page.goto(`${BASE}/surveys/${surveyId}`);
    for (const [key, text] of Object.entries(story.reword ?? {})) {
        const q = story.questions.find((q) => q.key === key);
        if (!q) throw new Error(`reword: no question with key ${key}`);
        const row = page.locator("li.question", { hasText: q.text });
        await row.locator("summary").click();
        await row.locator("input[name=text]").fill(text);
        await shoot(manifest, page, "question-editor-open", { kind: "card", locator: row });
        await row.getByRole("button", { name: "Save question" }).click();
        await page.waitForLoadState("networkidle");
    }
    await shoot(manifest, page, "question-reworded", {
        kind: "card",
        locator: card(page, "Questions"),
    });
    await publish(page, surveyId, 2);
    await shoot(manifest, page, "card-two-versions", {
        kind: "card",
        locator: card(page, "Published versions"),
    });
    await page.goto(`${BASE}/surveys/${surveyId}/audit`);
    await shoot(manifest, page, "audit", { kind: "viewport" });
}

/** Every published version's questions, keyed back to surveys.yaml by position. */
async function readVersions(surveyId: string, story: StoryDef): Promise<VersionRef[]> {
    const rows = await query<{
        version_id: string;
        number: number;
        id: string;
        identity_id: string;
        position: number;
        type: string;
        text: string;
    }>(`
    SELECT v.id AS version_id, v.number, q.id, q.question_identity_id AS identity_id,
           q.position, q.type, q.text
      FROM survey_versions v JOIN questions q ON q.version_id = v.id
     WHERE v.survey_id = ${lit(surveyId)}
     ORDER BY v.number, q.position`);
    const versions = new Map<number, VersionRef>();
    for (const r of rows) {
        const v = versions.get(r.number) ?? { number: r.number, id: r.version_id, questions: [] };
        const def = story.questions[r.position];
        if (!def || def.type !== r.type) {
            throw new Error(
                `${story.title} v${r.number}: question ${r.position} is ${r.type}, surveys.yaml expects ${def?.type}`,
            );
        }
        v.questions.push({
            key: def.key,
            id: r.id,
            identityId: r.identity_id,
            position: r.position,
            type: r.type,
            text: r.text,
        });
        versions.set(r.number, v);
    }
    return [...versions.values()].sort((a, b) => a.number - b.number);
}
