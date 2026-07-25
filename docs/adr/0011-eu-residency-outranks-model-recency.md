# EU residency outranks model recency

Every AI call stays pinned to `europe-west4`, even when that means using
an older model family. When a newer Gemini is available only at Vertex's
`global` location, we do not use it.

Concretely, as of 2026-07-25: `europe-west4` offers the Gemini 2.5
family (`gemini-2.5-flash`, `gemini-2.5-pro`, plus lite and TTS
variants). The 3.x family — `gemini-3.5-flash`, `gemini-3.6-flash`,
`gemini-3.1-pro-preview` and the rest — resolves **only** at
`locations/global`; probed and confirmed absent from europe-west1,
west3, west4, west9, north1, southwest1 and central2. So Earful runs
`gemini-2.5-flash` for generation, transcription and translation, and
`gemini-2.5-pro` for Insight Summaries.

## Considered Options

- **Use 3.x at `locations/global`**: the newest and cheapest-per-token
  models. But Vertex's global endpoint may serve a request from any
  region, so a respondent's spoken answer or written text could be
  processed outside the EU. That contradicts ADR-0004's europe-west4
  pin, the EU-hosting claim on `/trust`, and the region column of the
  sub-processor table — three places where we have told people something
  specific.
- **Split by operation**: voice stays in the EU, text operations go
  global. Better than moving everything, but answer text *is* respondent
  data, and a promise with a footnote about which sentences leave the EU
  is a worse promise than one without.
- **Stay in europe-west4 on the best model available there (chosen).**

## Consequences

- Model quality is a lagging indicator of what the platform offers, and
  that is accepted. The models in question are competent at the four
  things Earful asks of them: drafting questions, transcribing speech,
  translating, and summarising answers.
- **Upgrade trigger**: when any Gemini 3.x model becomes available in an
  EU region, switch to it. It is a configuration change — `AI_MODEL`
  and `AI_MODEL_ANALYZE` in tfvars — because nothing in the product
  names a model. Re-run
  `go test ./internal/ai -run Vertex_Integration` against the new id,
  including the transcription half, before pointing production at it.
- `gemini-live-2.5-flash-native-audio` exists in europe-west4 and would
  allow genuinely streaming transcription (partial transcripts while
  speaking) rather than the current buffer-then-transcribe. Not adopted:
  it would put local dev and cloud on different code paths, which M5
  deliberately avoided. Worth revisiting if respondents ask for it.
- If a future feature genuinely cannot be built without a global-only
  model, that is a new decision requiring its own ADR, a change to
  `/trust`, and a change to the sub-processor table — in that order, and
  before the feature ships.
