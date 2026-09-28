// The feature-tour deck generator.
//
//   deno task run                  every stage: reset, build, seed, screenshots, deck
//   deno task run --from seed      that stage and the ones after it
//   deno task run --only deck      one stage (deno task deck is the same)
//   deno task run --no-restore     leave the app on the run's AI environment afterwards
//
// The stages that drive the app (build, screenshots) start the mock model
// server, recreate the app container with the environment that points at
// it, and put the container back the way compose defines it when done,
// whether the run succeeded, failed or was interrupted.

import { parseArgs } from "@std/cli/parse-args";
import type { Browser } from "playwright";
import { launch } from "./browser.ts";
import { appEnvironment, useMock } from "./config.ts";
import { composeRestore, composeUp, waitHealthy } from "./compose.ts";
import { loadAI, loadSurveys } from "./content.ts";
import { Answers } from "./mock/answers.ts";
import { type MockServer, startMock } from "./mock/server.ts";
import { Manifest } from "./shots.ts";
import { build } from "./stages/build.ts";
import { deck } from "./stages/deck.ts";
import { reset } from "./stages/reset.ts";
import { screenshots } from "./stages/screenshots.ts";
import { seed } from "./stages/seed.ts";
import { loadState, type State } from "./state.ts";

const STAGES = ["reset", "build", "seed", "screenshots", "deck"] as const;
type Stage = typeof STAGES[number];

const args = parseArgs(Deno.args, {
    string: ["from", "only"],
    boolean: ["no-restore", "help"],
});

if (args.help) {
    console.log("usage: deno task run [--from <stage>] [--only <stage>] [--no-restore]");
    console.log("stages: " + STAGES.join(", "));
    Deno.exit(0);
}

function stageIndex(name: string | undefined, flag: string): number {
    if (!name) return -1;
    const i = STAGES.indexOf(name as Stage);
    if (i === -1) throw new Error(`${flag}: unknown stage "${name}" (one of ${STAGES.join(", ")})`);
    return i;
}

const only = stageIndex(args.only, "--only");
const from = stageIndex(args.from, "--from");
const selected: Stage[] = only !== -1 ? [STAGES[only]] : STAGES.slice(from === -1 ? 0 : from);

const surveys = await loadSurveys();
const ai = await loadAI();
const manifest = await Manifest.load();
const needsApp = selected.some((s) => s === "build" || s === "screenshots");
const needsBrowser = needsApp || selected.includes("deck");

let mock: MockServer | undefined;
let browser: Browser | undefined;
let appChanged = false;

async function restore() {
    if (browser) await browser.close().catch(() => {});
    if (mock) await mock.stop().catch(() => {});
    if (appChanged && !args["no-restore"]) {
        console.log("restoring the app container to its compose defaults");
        await composeRestore().catch((e) => console.error(e));
    }
}

Deno.addSignalListener("SIGINT", async () => {
    console.log("\ninterrupted");
    await restore();
    Deno.exit(130);
});

try {
    if (needsApp) {
        if (useMock) mock = startMock(new Answers(surveys, ai));
        console.log("bringing the app up with the run's AI environment");
        await composeUp(["app", "mailpit"], appEnvironment());
        appChanged = true;
        await waitHealthy();
    } else if (selected.includes("reset") || selected.includes("seed")) {
        await composeUp(["postgres", "mailpit"]);
    }
    if (needsBrowser) browser = await launch();

    let state: State | undefined;
    for (const stage of selected) {
        console.log(`\n== ${stage}`);
        switch (stage) {
            case "reset":
                await reset(surveys);
                break;
            case "build":
                state = await build(browser!, surveys, ai, manifest);
                break;
            case "seed":
                state ??= await loadState();
                await seed(surveys, state);
                break;
            case "screenshots":
                state ??= await loadState();
                await screenshots(browser!, surveys, state, manifest);
                break;
            case "deck":
                await deck(browser!, manifest);
                break;
        }
    }
} finally {
    await restore();
}
