# Voice is transcript-only; audio is never stored

Respondents may answer text questions by speaking. Audio is transcribed and immediately discarded — never written to disk, object storage, logs, or the database, in any environment. Recognition prefers verifiably-local browser recognition (feature-detected, e.g. `processLocally`); otherwise audio streams through our server to Vertex AI Gemini pinned to europe-west4 and only the transcript survives. The respondent reviews and can edit the transcript before submitting; typing is always available.

## Considered Options

- Store recordings with a retention window (what the homepage currently implies): enables replay features, but puts voice data at rest — the largest GDPR liability the product could carry, against the founding instinct.
- Browser-API-first regardless of locality: cheaper, but silently routes audio through Google/Apple servers without disclosure.
- Transcript-only, local-first (chosen).

## Consequences

- No replay/playback features can ever be promised; summaries and analysis operate on transcripts.
- tryearful.com copy must change: "keep every recording under your control" → "your voice is never stored".
- Non-local browser recognition is treated as unavailable; the server fallback path must therefore be production-grade from day 1, and its cost bot-protected.
- Vertex AI (europe-west4), not the consumer Gemini API, is the only server-side model path.
