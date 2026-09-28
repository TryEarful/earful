// out/state.json: what the build stage created, for the stages after it.

import { join } from "@std/path";
import { OUT_DIR } from "./config.ts";

export interface QuestionRef {
    /** The key from surveys.yaml, matched by position within the version. */
    key: string;
    id: string;
    identityId: string;
    position: number;
    type: string;
    text: string;
}

export interface VersionRef {
    number: number;
    id: string;
    questions: QuestionRef[];
}

export interface ParticipantRef {
    email: string;
    id: string;
    inviteUrl?: string;
}

export interface StoryState {
    email: string;
    userId: string;
    workspaceId: string;
    surveyId: string;
    shareUrl: string;
    storageState: string;
    versions: VersionRef[];
    participants: ParticipantRef[];
    seeded?: { responses: number; from: string; to: string };
}

export interface State {
    createdAt: string;
    baseUrl: string;
    stories: Record<string, StoryState>;
}

const FILE = join(OUT_DIR, "state.json");

export async function loadState(): Promise<State> {
    try {
        return JSON.parse(await Deno.readTextFile(FILE)) as State;
    } catch {
        throw new Error("out/state.json is missing: run the build stage first (--from build)");
    }
}

export async function saveState(state: State) {
    await Deno.mkdir(OUT_DIR, { recursive: true });
    await Deno.writeTextFile(FILE, JSON.stringify(state, null, 2) + "\n");
}

export async function removeState() {
    await Deno.remove(FILE).catch(() => {});
}

export function latest(story: StoryState): VersionRef {
    return story.versions[story.versions.length - 1];
}

export function question(version: VersionRef, key: string): QuestionRef {
    const found = version.questions.find((q) => q.key === key);
    if (!found) throw new Error(`no question with key "${key}" in version ${version.number}`);
    return found;
}
