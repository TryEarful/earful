# The EU pin is Vertex's `eu` multi-region endpoint, on Gemini 3.8 Flash

Amends ADR-0011. Every AI call is still processed inside the EU and the
global endpoint is still never used; what changes is *which* endpoint
carries that promise, and which model runs behind it.

As of 2026-09-28, Earful calls Vertex at the `eu` multi-region
(`https://aiplatform.eu.rep.googleapis.com`, `locations/eu`) and runs
`gemini-3.8-flash` for every operation: transcription, question drafting,
translation and Insight Summaries. `VERTEX_LOCATION` and the model ids
remain configuration.

## Why now

`gemini-2.5-flash` and `gemini-2.5-pro`, the models ADR-0011 chose, retire
on 2026-10-20. Nothing in the 3.x family is offered at europe-west4, so the
retirement forced the question ADR-0011 deferred.

## What the documentation actually guarantees

Three findings from Google's data-residency, zero-data-retention and
abuse-monitoring pages, read on 2026-09-28:

1. **The terms are platform-wide, not per model.** The training
   restriction covers "all managed models … including GA and pre-GA
   models". Abuse-monitoring logging is the same for every Gemini model:
   only when a safety classifier flags a prompt, kept up to 90 days in the
   customer's chosen region, never used for training, and opt-out by
   form. The stricter 30-day logging under the "Advanced AI Safety
   Addendum" applies to designated partner models, not to Gemini. The one
   per-call retention is a 24-hour **in-memory** cache that Google states
   respects residency and zero-data-retention; it can be switched off per
   project. So moving to a newer Gemini changes none of the promises on
   `/trust`.
2. **A single region never carried the guarantee we thought it did.**
   Google's per-model table of ML-processing commitments has an empty
   Netherlands column for every Gemini model, including the 2.5 Flash we
   ran. What a regional endpoint documents is processing "within the
   broader multi-regional … jurisdiction associated with that region",
   which for europe-west4 is the EU. The promise that held in practice was
   "inside the EU", and the `eu` multi-region endpoint is the one place
   Google writes that promise down: ML processing in EU member states,
   with the UK and Switzerland explicitly excluded.
3. **The 3.x Flash tier has EU processing only at `eu`.** Gemini 3.5, 3.6,
   3.7 and 3.8 Flash all list the `eu` multi-region on the residency table;
   only 3.5 Flash also lists a single EU region (Frankfurt). No Pro tier
   has EU processing.

## Considered Options

- **`gemini-3.5-flash` at europe-west3 (Frankfurt)**: keeps a single-region
  pin, but moves the named country, locks the product to the one 3.x model
  with such a region, and documents no stronger guarantee than `eu` does.
- **A dedicated speech model.** `gemini-3.5-transcribe` (2.6% word error
  rate, language hints) would be the better transcriber, but is offered
  only at the global endpoint, with regional support "coming soon".
- **The Live API for real-time transcripts.** `gemini-3.8-live` is
  available at `eu`, but it is a conversational model that wants to reply,
  needs a second protocol beside the buffered path, and was not yet on the
  residency table at the time of writing. Considered and set aside; the
  buffered path stays.
- **`gemini-3.8-flash` at the `eu` multi-region (chosen).** GA, no
  retirement date, audio input through the same `streamGenerateContent`
  call the client already makes, documented EU processing.

## Consequences

- `/trust`, the sub-processor table and SPEC.md say "EU multi-region,
  processed only in EU member states" instead of naming a region. The
  hosting row still names europe-west4: the service, its database and its
  backups have not moved.
- The Vertex client knows the multi-region host shape; a multi-region name
  on the regional host shape resolves to nothing, so this is code, not
  configuration. `VERTEX_LOCATION` is decoupled from the Cloud Run region
  in Terraform (`ai_location`).
- Transcription asks the model for its lowest thinking level so the
  transcript streams without a reasoning preamble; the other operations
  keep the default. The client derives that from the model id, not from
  the operation: `thinkingLevel` goes out only to a Gemini 3.x Flash or
  Pro id, and any other id gets the plain request, because Gemini 2.5
  rejects the field with a 400 and no transcript. A model change is
  therefore a tfvars change and nothing else.
- The rollout order is apply, then tag. The pipeline moves only the
  image, so a binary that expects the new location or model must not
  reach production before the environment does; `make release-check`
  refuses the tag while either env has an unapplied plan.
- Insight Summaries run on the same Flash model as everything else until an
  EU-resident stronger tier exists. That is a tfvars change.
- **Upgrade trigger**: the day `gemini-3.5-transcribe`'s model card lists
  `eu`, set `AI_MODEL_TRANSCRIBE` to it, after the integration test's voice
  half passes against it.
- **Retention hardening** (runbook): switch off the 24-hour in-memory cache
  on staging and production and file the abuse-logging opt-out. Neither is
  required for the promises made, both are cheap, and both match the
  spirit of ADR-0004. Session resumption, which would cache audio at rest,
  is a Live API feature and is not used.
- Before any future model change: confirm the id has an EU-processing entry
  on the residency table, run the integration test against it, and check
  no gitignored tfvars still pins a retired id.
