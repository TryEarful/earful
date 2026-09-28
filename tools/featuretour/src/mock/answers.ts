// What the mock model says. Each request is classified by the system
// prompt the app sends (internal/http/generate.go, internal/http/insights.go,
// internal/ai/models.go) and answered from the content files.

import type { AIContent, Surveys } from "../content.ts";

export interface Message {
    role: string;
    content: string;
}

/** Language names the app uses (domain.LanguageName) mapped back to codes. */
const LANGUAGE_CODES: Record<string, string> = {
    english: "en",
    spanish: "es",
    dutch: "nl",
    german: "de",
    french: "fr",
    italian: "it",
    portuguese: "pt",
    polish: "pl",
    swedish: "sv",
    danish: "da",
};

export class Answers {
    constructor(private surveys: Surveys, private ai: AIContent) {}

    /** The reply text for a chat completion, streamed by the server. */
    reply(messages: Message[]): string {
        const system = messages.find((m) => m.role === "system")?.content ?? "";
        const user = [...messages].reverse().find((m) => m.role === "user")?.content ?? "";
        if (system.startsWith("You write survey questions")) return this.draft(user);
        if (system.startsWith("You are a research analyst")) return this.insights(user);
        if (system.startsWith("You are a translator")) return this.translate(system, user);
        return "OK";
    }

    transcript(): string {
        return this.ai.transcript;
    }

    /** One JSON object per line, the shape the generation prompt asks for. */
    private draft(prompt: string): string {
        const lower = prompt.toLowerCase();
        const story = Object.values(this.surveys.stories).find((s) =>
            lower.includes(s.prompt_keyword.toLowerCase())
        );
        if (!story) return "";
        const lines = story.questions.map((q) => {
            const line: Record<string, unknown> = {
                type: q.type,
                text: q.text,
                required: q.required ?? false,
            };
            if (q.options) line.options = q.options;
            if (q.type === "rating_scale" && q.scale) {
                line.scale_min = q.scale[0];
                line.scale_max = q.scale[1];
            }
            return JSON.stringify(line);
        });
        return lines.join("\n") + "\n";
    }

    /** The prompt opens with "Survey: <title>"; the title picks the story. */
    private insights(prompt: string): string {
        const first = prompt.split("\n", 1)[0].replace(/^Survey:\s*/, "").trim();
        for (const [name, story] of Object.entries(this.surveys.stories)) {
            if (story.title === first && this.ai.insights[name]) return this.ai.insights[name];
        }
        const fallback = Object.values(this.ai.insights)[0];
        return fallback ?? "No summary is prepared for this survey.";
    }

    /** "Translate the user's text from X to <Language>." names the target. */
    private translate(system: string, text: string): string {
        const match = system.match(/ to ([^.]+)\./);
        const name = (match?.[1] ?? "").trim().toLowerCase();
        const code = LANGUAGE_CODES[name] ?? name;
        const dictionary = this.ai.translations[code] ?? {};
        const key = text.split(/\s+/).join(" ").trim();
        return dictionary[key] ?? text;
    }
}
