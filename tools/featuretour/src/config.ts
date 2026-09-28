// Paths, endpoints and the environment the app is given for a run.
//
// Everything here is read once; a stage never reaches for Deno.env itself,
// so the README can list the whole surface in one place.

import { dirname, fromFileUrl, join, resolve } from "@std/path";

export const TOOL_ROOT = resolve(dirname(fromFileUrl(import.meta.url)), "..");
export const REPO_ROOT = resolve(TOOL_ROOT, "..", "..");
export const CONTENT_DIR = join(TOOL_ROOT, "content");
export const OUT_DIR = join(TOOL_ROOT, "out");
export const SHOTS_DIR = join(OUT_DIR, "shots");
export const AUTH_DIR = join(OUT_DIR, "auth");
export const RESULTS_DIR = join(OUT_DIR, "results");

const env = (name: string, fallback: string) => Deno.env.get(name) ?? fallback;

/** The app as the browser reaches it. */
export const BASE = env("FEATURETOUR_BASE_URL", "http://localhost:8080");
/** mailpit's HTTP API and inbox UI. */
export const MAILPIT = env("FEATURETOUR_MAILPIT_URL", "http://localhost:8025");
/** The mock model server; the app container reaches it through host.docker.internal. */
export const MOCK_PORT = 11435;
export const MOCK_MODEL = "local-model";

/** Shown on /trust. Placeholders, so the deck never states a real address. */
export const HOSTING_REGION = env("HOSTING_REGION", "Hetzner, Falkenstein (Germany)");
export const CONTACT_EMAIL = env("CONTACT_EMAIL", "privacy@example.org");

/** Headed browser for watching a run; screenshots are identical either way. */
export const HEADED = Deno.env.get("FEATURETOUR_HEADED") === "1";

/** A real model replaces the mock when AI_BASE_URL is set. */
export const REAL_AI_BASE_URL = Deno.env.get("AI_BASE_URL");
export const useMock = !REAL_AI_BASE_URL;

/** Environment the app container runs with for the duration of a run. */
export function appEnvironment(): Record<string, string> {
    const ai = useMock
        ? {
            AI_PROVIDER: "openai",
            AI_BASE_URL: `http://host.docker.internal:${MOCK_PORT}/v1`,
            AI_MODEL: MOCK_MODEL,
            TRANSCRIBE_PROVIDER: "openai",
        }
        : {
            AI_PROVIDER: "openai",
            AI_BASE_URL: REAL_AI_BASE_URL!,
            AI_MODEL: env("AI_MODEL", ""),
            TRANSCRIBE_PROVIDER: env("TRANSCRIBE_PROVIDER", "openai"),
        };
    return { ...ai, HOSTING_REGION, CONTACT_EMAIL };
}

/** The keys appEnvironment sets; removing them restores compose's defaults. */
export const APP_ENV_KEYS = [
    "AI_PROVIDER",
    "AI_BASE_URL",
    "AI_MODEL",
    "TRANSCRIBE_PROVIDER",
    "HOSTING_REGION",
    "CONTACT_EMAIL",
];

export function today(): string {
    return new Date().toISOString().slice(0, 10);
}
