# Workspace export format

**Format version 3.**

Treat this document as the stable description of the format, not as
notes that drift. A workspace export is what makes "you can leave" true
rather than reassuring, so the shape below changes by version bump and a
note in this file, never quietly. The import tool (the first post-MVP
ticket) will be written against exactly this.

Produce one from **Account → Export everything**. You get a zip:

```
workspace.json                   everything, in one document
surveys/<slug>-<id8>.csv         one CSV per survey (the same file the
                                 survey's own Download CSV produces)
README.txt                       what a person needs to know
```

`workspace.json` is the complete record. The CSVs are a convenience for
spreadsheets and contain nothing the JSON doesn't.

## workspace.json

```jsonc
{
  "format_version": 3,
  "exported_at": "2026-07-25T14:03:11Z",
  "workspace": { "id": "uuid", "name": "sam's workspace" },
  "surveys": [
    {
      "id": "uuid",
      "title": "Onboarding feedback",
      "is_anonymous": true,
      "status": "Open",                    // Draft | Open | Closed, derived
      "created_at": "2026-07-01T09:12:00Z",
      "close_at": "2026-08-01T00:00:00Z",  // omitted when unset
      "closed_at": null,                   // omitted when unset

      "versions": [
        {
          "number": 1,
          "published_at": "2026-07-02T10:00:00Z",
          "questions": [
            {
              "identity_id": "uuid",       // the Question Identity — see below
              "position": 1,
              "type": "long_text",
              "text": "What stood out in your first week?",
              "required": true,
              "options": ["…"],            // choice/dropdown types only
              "scale_min": 1,              // rating_scale and nps only
              "scale_max": 7
            }
          ]
        }
      ],

      "participants": [                    // invited surveys only
        {
          "email": "someone@example.com",
          "invited_at": "2026-07-02T10:05:00Z",
          "submitted_at": "2026-07-02T18:22:00Z",
          "bounced_at": null
        }
      ],

      "responses": [
        {
          "id": "uuid",
          "version": 1,                    // the version this response was served
          "submitted_at": "2026-07-02T18:22:00Z",
          "duration_secs": 143,            // omitted when unknown
          "participant_email": "…",        // invited surveys only
          "answers": {
            "<identity_id>": { "text": "Setup was quick." }
          }
        }
      ],

      "stats": [                           // unlinked counters, ADR-0009
        { "metric": "start", "count": 58 },
        { "metric": "browser", "bucket": "Chrome", "count": 31 }
      ],

      "stats_daily": [                     // dated flow counters, ADR-0012
        { "metric": "start", "day": "2026-07-03", "count": 4 },
        { "metric": "completion", "day": "2026-07-03", "count": 2 },
        { "metric": "reached", "bucket": "<identity_id>", "day": "2026-07-03", "count": 2 }
      ]
    }
  ]
}
```

### Counters

`stats` holds undated totals. `stats_daily` holds the same kind of
count with a UTC `day`, for the three flow metrics only: `start` (the
survey page was opened), `completion` (a response was submitted) and
`reached` (the last question a submitted response answered, `bucket`
being its `identity_id`). Browser, device and country are never dated,
so they appear in `stats` only. A survey that existed before per-day
counting began has its earlier opens and submissions in `stats` and
its later ones in `stats_daily`; the two are added, never overlapping.
The `reached` rows in `stats` are keyed by question position, an older
shape kept as it was; the dated rows are keyed by identity.

### Answer values

An answer object carries exactly one field, chosen by the question type:

| Question type | Field | Example |
|---|---|---|
| `long_text`, `short_text` | `text` | `{"text": "It was fine."}` |
| `single_choice`, `dropdown` | `choice` | `{"choice": "Weekly"}` |
| `multiple_choice` | `choices` | `{"choices": ["Email", "Slack"]}` |
| `rating_scale`, `nps` | `number` | `{"number": 7}` |
| `yes_no` | `bool` | `{"bool": true}` |
| `date` | `date` | `{"date": "2026-04-18"}` |

A `date` is a calendar day in ISO 8601 form, `yyyy-mm-dd`, with no time
and no time zone: the day the respondent picked, not an instant.

A question a respondent skipped has **no entry** in `answers`. That is
deliberate and worth preserving on import: it is what distinguishes "left
blank" from "answered with nothing".

## The one thing an importer must not lose

`identity_id` is the **Question Identity**. It is what makes results
comparable when a question is reworded: version 1 asking "How was it?"
and version 2 asking "Looking back, how was it?" are the same question,
and every response points at the identity rather than at a wording.

An importer that generates fresh identities per version will silently
split one question's history into two, and no error will ever be raised.
Preserve them.

Responses point at the version they were served (`version`), never at the
newest one. Nothing in Earful copies a response forward; results are
folded across versions at read time (ADR-0001), and an importer should
keep that shape.

## What is deliberately absent

Anonymous responses carry **no email address, no IP address and no device
details** — not because the export withholds them, but because no such
column exists anywhere near a response (ADR-0003). The audience counters
under `stats` are survey-level and have no join path to any response
(ADR-0009).

Voice answers appear as ordinary text: audio is transcribed and discarded
in the same request and has never existed anywhere to export (ADR-0004).

## Downloads

An export is built in the background and downloadable for **24 hours**.
The link requires a session in the owning workspace, so it is not a
bearer token; after it expires, build a fresh one.

Closing an account offers the same archive as a parting copy ("Send me
a copy of my data" on the delete form). Closing revokes every session,
so that link is a bearer token instead: it is emailed to the address
the account had, only its SHA-256 is stored, and it works for **7 days**
from the moment the account closed. If the archive cannot be built, or
is over the cap below, the email says so instead of carrying a link.

A workspace whose archive would exceed 64 MB fails with a message
explaining that, rather than trying to push it through a database row —
see ADR-0010 for why the archive lives in Postgres at all.

## Changes

| Version | Change |
|---|---|
| 3 | The `date` question type, and its `date` answer field. Everything in version 2 is unchanged. |
| 2 | `stats_daily` added: per-day opened, submitted and reached counters (ADR-0012). Everything in version 1 is unchanged. |
| 1 | First published format (M7-T3). |
