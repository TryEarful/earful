// The three content files, parsed and checked. Wording lives there; the
// code only knows the shapes.

import { join } from "@std/path";
import { parse as parseYAML } from "@std/yaml";
import { CONTENT_DIR } from "./config.ts";

// --- surveys.yaml -----------------------------------------------------------

export type QuestionType =
    | "long_text"
    | "short_text"
    | "single_choice"
    | "multiple_choice"
    | "dropdown"
    | "rating_scale"
    | "nps"
    | "yes_no";

export interface AnswerSpec {
    /** Value -> relative weight (option text, scale point, or yes/no). */
    weights?: Record<string, number>;
    /** Open-text answers, used in shuffled order before repeating. */
    pool?: string[];
    /** For multiple_choice: how many options one response ticks, [min, max]. */
    pick?: [number, number];
    /** Share of responses that leave the question unanswered (never for required). */
    skip_rate?: number;
}

export interface QuestionDef {
    key: string;
    type: QuestionType;
    text: string;
    options?: string[];
    scale?: [number, number];
    required?: boolean;
    answers?: AnswerSpec;
}

export interface StatsSpec {
    opened_per_response: number;
    browser: Record<string, number>;
    device: Record<string, number>;
    country: Record<string, number>;
}

export interface StoryDef {
    email: string;
    workspace: string;
    title: string;
    anonymity: "anonymous" | "invited";
    prompt: string;
    prompt_keyword: string;
    created_days_ago: number;
    spread_days: number;
    languages?: string[];
    participants?: string[];
    submitted?: string[];
    super_admin?: boolean;
    questions: QuestionDef[];
    /** Question key -> new wording, published as version 2. */
    reword?: Record<string, string>;
    responses?: number;
    responses_by_version?: Record<string, number>;
    stats: StatsSpec;
    /** What the live respondent types during the screenshot run. */
    respondent_answer: string;
}

export interface Surveys {
    stories: Record<string, StoryDef>;
}

export async function loadSurveys(): Promise<Surveys> {
    const raw = parseYAML(await Deno.readTextFile(join(CONTENT_DIR, "surveys.yaml"))) as Surveys;
    for (const [name, story] of Object.entries(raw.stories)) {
        if (!story.questions?.length) {
            throw new Error(`surveys.yaml: story ${name} has no questions`);
        }
        const keys = new Set<string>();
        for (const q of story.questions) {
            if (keys.has(q.key)) {
                throw new Error(`surveys.yaml: ${name} repeats question key ${q.key}`);
            }
            keys.add(q.key);
            if (q.answers?.skip_rate && q.required) {
                throw new Error(`surveys.yaml: ${name}.${q.key} is required and has a skip_rate`);
            }
        }
        if (story.anonymity === "invited" && !story.participants?.length) {
            throw new Error(`surveys.yaml: invited story ${name} lists no participants`);
        }
        if (story.responses === undefined && !story.responses_by_version) {
            throw new Error(`surveys.yaml: ${name} needs responses or responses_by_version`);
        }
    }
    return raw;
}

// --- ai.md ------------------------------------------------------------------

export interface AIContent {
    /** Story name -> the Insight Summary the mock returns. */
    insights: Record<string, string>;
    /** Language code -> source text -> translation. */
    translations: Record<string, Record<string, string>>;
    /** What every spoken answer transcribes to. */
    transcript: string;
}

export async function loadAI(): Promise<AIContent> {
    const text = await Deno.readTextFile(join(CONTENT_DIR, "ai.md"));
    const content: AIContent = { insights: {}, translations: {}, transcript: "" };
    const sections = text.split(/^## /m).slice(1);
    for (const section of sections) {
        const nl = section.indexOf("\n");
        const heading = section.slice(0, nl).trim();
        const body = section.slice(nl + 1).trim();
        const [kind, key] = heading.split(":").map((s) => s.trim());
        if (kind === "insights" && key) {
            content.insights[key] = body;
        } else if (kind === "translations" && key) {
            const fenced = body.match(/```yaml\n([\s\S]*?)```/);
            if (!fenced) throw new Error(`ai.md: "${heading}" needs a fenced yaml block`);
            content.translations[key] = parseYAML(fenced[1]) as Record<string, string>;
        } else if (kind === "transcript") {
            content.transcript = body;
        } else {
            throw new Error(`ai.md: unknown section "${heading}"`);
        }
    }
    if (!content.transcript) throw new Error("ai.md: missing a transcript section");
    return content;
}

// --- deck.md ----------------------------------------------------------------

export type Layout = "cover" | "section" | "text" | "side" | "two" | "one" | "code";

export interface SlideDef {
    id: string;
    layout: Layout;
    shots: string[];
    columns?: number;
    /** For layout code: a file under out/ (or an absolute path) to show. */
    code?: string;
    /** For layout code: lines to keep from the top of the file. */
    lines?: number;
    /** The markdown body without the caption list. */
    body: string;
    /** Filled by the deck stage from `code`. */
    codeText?: string;
    /** One caption per shot, in order. */
    captions: string[];
}

const LAYOUTS: Layout[] = ["cover", "section", "text", "side", "two", "one", "code"];
const MAX_SHOTS: Record<Layout, number> = {
    cover: 0,
    section: 0,
    text: 0,
    side: 1,
    two: 2,
    one: 1,
    code: 1,
};

export async function loadDeck(): Promise<SlideDef[]> {
    const text = await Deno.readTextFile(join(CONTENT_DIR, "deck.md"));
    const lines = text.split("\n");
    const slides: SlideDef[] = [];
    let i = 0;
    // Skip anything before the first block (a comment, a title).
    while (i < lines.length && lines[i].trim() !== "---") i++;
    while (i < lines.length) {
        if (lines[i].trim() !== "---") throw new Error(`deck.md line ${i + 1}: expected ---`);
        i++;
        const yamlLines: string[] = [];
        while (i < lines.length && lines[i].trim() !== "---") yamlLines.push(lines[i++]);
        if (i >= lines.length) throw new Error("deck.md: unterminated front matter");
        i++;
        const bodyLines: string[] = [];
        while (i < lines.length && lines[i].trim() !== "---") bodyLines.push(lines[i++]);
        const meta = (parseYAML(yamlLines.join("\n")) ?? {}) as Partial<SlideDef>;
        slides.push(makeSlide(meta, bodyLines));
    }
    const ids = new Set<string>();
    for (const slide of slides) {
        if (ids.has(slide.id)) throw new Error(`deck.md: slide id "${slide.id}" is used twice`);
        ids.add(slide.id);
    }
    return slides;
}

function makeSlide(meta: Partial<SlideDef>, bodyLines: string[]): SlideDef {
    if (!meta.id) throw new Error("deck.md: a slide is missing its id");
    const layout = meta.layout as Layout;
    if (!LAYOUTS.includes(layout)) {
        throw new Error(`deck.md: slide ${meta.id} has unknown layout "${meta.layout}"`);
    }
    const shots = meta.shots ?? [];
    if (shots.length > MAX_SHOTS[layout]) {
        throw new Error(
            `deck.md: slide ${meta.id} (${layout}) allows ${
                MAX_SHOTS[layout]
            } shots, has ${shots.length}`,
        );
    }
    // Trailing "- " lines are the captions, one per shot.
    const trimmed = [...bodyLines];
    while (trimmed.length && trimmed[trimmed.length - 1].trim() === "") trimmed.pop();
    const captions: string[] = [];
    while (trimmed.length && /^- /.test(trimmed[trimmed.length - 1])) {
        captions.unshift(trimmed.pop()!.replace(/^- /, "").trim());
    }
    if (shots.length > 0 && captions.length !== shots.length) {
        throw new Error(
            `deck.md: slide ${meta.id} has ${shots.length} shots but ${captions.length} captions`,
        );
    }
    if (shots.length === 0 && captions.length > 0) {
        // Not captions after all: a plain list belongs to the body.
        trimmed.push(...captions.map((c) => "- " + c));
        captions.length = 0;
    }
    return {
        id: meta.id,
        layout,
        shots,
        columns: meta.columns,
        code: meta.code,
        lines: meta.lines,
        body: trimmed.join("\n").trim(),
        captions,
    };
}
