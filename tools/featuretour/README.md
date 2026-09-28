# Feature tour

A PDF walkthrough of Earful, built from screenshots of the compose stack: three demo accounts, three
surveys made through the product, a month of seeded responses, and a 16:9 deck printed by Chromium.

```sh
make featuretour        # everything: reset, build, seed, screenshots, deck (about 15 minutes)
make featuretour-deck   # only the PDF, from the screenshots of the last run (seconds)
```

The PDF lands in `out/` as `earful-feature-tour-<date>.pdf`. Nothing under `out/` is committed.

## Prerequisites

- Docker, for the compose stack (`docker compose --profile app`).
- Deno 2.x. The tool is TypeScript run by Deno; it is not needed to build, test or run Earful
  itself.
- Playwright's Chromium. `make e2e-smoke` installs it; otherwise
  `deno run -A npm:playwright@1.61.1 install chromium`. The version is pinned to the one the e2e
  suite resolves so both share one browser.

The scripts run with `-A`: they spawn Docker and a browser and write under `out/`, and a narrower
allow-list would have to name the browser binary and the temp directory for nothing gained.

## What a run does

| Stage         | What it touches                                                                                                                                                                                                                                                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `reset`       | Soft-deletes the demo accounts' earlier surveys, clears their spent magic-link tokens and export jobs, empties mailpit, wipes `out/`. Only the three accounts in `content/surveys.yaml` are affected.                                                                                                                                 |
| `build`       | Signs each account in by magic link, names its workspace, creates the survey, drafts questions with the model, adds and reviews a localization, publishes, invites participants, rewords a question into version 2. Takes the screenshots that only exist mid-flow. Writes `out/state.json` and saves each session under `out/auth/`. |
| `seed`        | Writes responses, answers and the stats counters straight into Postgres, deterministically from the answer pools and weights, and backdates the surveys so the stats pages show a month. Reruns replace the data rather than add to it.                                                                                               |
| `screenshots` | Every other capture: results, insights, translations, stats, invited links, voice, admin, phone widths. Appends to `out/shots.json`.                                                                                                                                                                                                  |
| `deck`        | `content/deck.md` plus `out/shots.json` become `out/deck.html`, printed to the PDF. Fails if a slide refers to a screenshot that was not taken, a caption count is off, or a slide overflows.                                                                                                                                         |

`deno task run --from <stage>` runs a stage and the ones after it; `--only <stage>` runs one. The
build stage refuses to run on accounts that already hold surveys, so a rerun starts from `reset`.

The build and screenshot stages recreate the app container with the AI environment they need and put
it back to the compose defaults when they finish, fail or are interrupted. `--no-restore` leaves it
as is, for looking at the demo data afterwards; `docker compose --profile app up -d
app` restores it
by hand.

## The model

By default a small server inside the tool answers the app's model calls with prepared text
(`content/ai.md`) so every run produces the same deck. It speaks the same API as Ollama and
llamafile, and the app is pointed at it with `AI_PROVIDER=openai`.

To use a real local model instead, set `AI_BASE_URL` (and `AI_MODEL`):

```sh
AI_BASE_URL=http://host.docker.internal:11434/v1 AI_MODEL=gemma4 make featuretour
```

The mock is then not started. Insight Summaries, drafted questions and translations come from the
model, and the deck varies between runs. Voice still transcribes through the same server
(`TRANSCRIBE_PROVIDER=openai`); set `TRANSCRIBE_PROVIDER` to override.

## Changing the words

Everything a reader sees is in `content/`:

- `deck.md`: the slides. Each block has front matter (id, layout, screenshots) and a markdown body
  with the title, the prose, and one caption line per screenshot. The file's opening comment lists
  the layouts. After editing, `make featuretour-deck` rebuilds the PDF without touching the stack.
- `surveys.yaml`: the three stories: accounts, questions, answer pools, weights and audience
  buckets. The model "drafts" exactly these questions, and the seed stage answers them. Changing a
  question changes what the screenshots show, so a full run follows.
- `ai.md`: the Insight Summaries the mock returns, the translation dictionaries, and the transcript
  of a spoken answer.

Screenshot ids are stable: `result-<story>-<key>`, `respond-<story>-<key>`, `stats-<story>-trend`,
and so on. `out/shots.json` lists every id a run produced.

## Configuration

| Variable                                         | Default                 | Purpose                                      |
| ------------------------------------------------ | ----------------------- | -------------------------------------------- |
| `FEATURETOUR_BASE_URL`                           | `http://localhost:8080` | Where the browser reaches the app            |
| `FEATURETOUR_MAILPIT_URL`                        | `http://localhost:8025` | mailpit's API and inbox                      |
| `FEATURETOUR_HEADED`                             | unset                   | `1` shows the browser                        |
| `AI_BASE_URL`, `AI_MODEL`, `TRANSCRIBE_PROVIDER` | unset                   | A real model instead of the mock             |
| `HOSTING_REGION`, `CONTACT_EMAIL`                | placeholders            | What the trust page shows in the screenshots |

## Layout rules

Slides are 1920 by 1080. A slide carries at most two screenshots. Image boxes have fixed sizes per
layout and no shadows (a PDF viewer may draw a shadow as an opaque rectangle). How a screenshot
fills its box follows from its kind, recorded by the screenshots stage: an element capture fills the
width; a page capture is scaled to its content column and clipped; a phone capture fills the height.
No image is sized by hand.
