---
status: ready-for-agent
source: grilling session 2026-07-12/15; updated 2026-07-19 from "Voice questionnaires app details 002" — see PLAN.md, CONTEXT.md, docs/adr/0001–0011
pending: EU AI Act additions (companion doc) — separate pass
implementation: "Every milestone complete except M9's launch acts: M0–M8, M10, M11 and M12 shipped, and as of 2026-07-27 M9-T2/T3/T4 are closed too — the alert checklist, the rate-limit soak and the rollback drill. Open: M9-T5 (launch) alone. See PLAN.md's Status section for per-ticket progress"
---

# Earful MVP — Specification

## Implementation status

Granular, per-ticket status lives in [PLAN.md](PLAN.md)'s Status section —
this is the single source of truth, kept aligned with this document as
work lands. **Every milestone is complete except the launch itself:**

- **Implemented and deployed**: M0 foundations, M1 cloud and deploy
  pipeline, M2 auth and workspaces, M3 survey building, M4 answering
  (anonymous and invited, including live email delivery), M5 voice
  (transcript-only, ADR-0004), M6 AI question generation, M7
  results/exports/stats, M8 data lifecycle and trust, M10 Insight
  Summaries, M11 localization and answer translation, M12's private-beta
  gate, and M9's operational half — founder metrics, the full alert set,
  the security pass and a runbook whose every procedure has now been
  executed at least once. Every numbered story below carries a
  `[tested]` link.
- **Open**: M9-T5 alone — the launch itself: the homepage copy fix in the
  marketing repository, a feedback survey answered by voice on a phone,
  and the announcement.

Execution order in practice: M0 → M2 → M3 → M4 → M6-T1/T2 → M1+M9 (cloud)
→ M12 → M5 → M6-T3 → M7 → M8 → M10 → M11 → M9-T5 (launch).

## Problem Statement

Teams that need honest feedback don't trust closed survey SaaS with their respondents' data, and respondents don't finish surveys that make them type. Existing tools (Typeform et al.) are proprietary, US-hosted, and text-first: creators can't self-host or leave with their data, privacy-sensitive organizations can't adopt them, and open-text answers — the valuable ones — go unanswered because typing is friction. Researchers additionally drown in raw answers: reading hundreds of free-text replies to find themes is manual work, and language barriers shrink who they can ask. There is no open-source, EU-hosted, voice-first survey tool that people can pay for as a service *and* walk away from with their data.

## Solution

Earful: an open-source (AGPL-3.0) survey platform, hosted in the EU (europe-west4), where respondents can *speak* their answers — transcribed instantly, with the audio never stored — and creators build surveys with AI assistance. Creators run anonymous or invited surveys in ten question types, localize questions into their respondents' languages, read answers translated back into their own, and get AI Insight Summaries — themes, patterns, representative quotes — across the whole respondent population. Append-only versioning never destroys or misattributes a response; results aggregate across versions; survey stats and privacy-preserving audience aggregates close the improvement loop without tracking anyone. Creators can export their entire workspace at any time to move to a self-hosted instance. Trust is the product: no third-party scripts on respondent pages, strong anonymity guarantees, transparent processor list, immutable backups.

## User Stories

### Accounts, workspaces and access

1. As a survey creator, I want to log in with my Google account, so that I can start without creating another password. [tested](internal/http/auth_google_test.go)
2. As a survey creator, I want to log in by receiving a magic link by email, so that I can use Earful without a Google account. [tested](internal/http/auth_magic_test.go)
3. As a survey creator, I want a personal Workspace created automatically on first login, so that I can create surveys immediately. [tested](internal/http/workspace_test.go)
4. As a workspace member, I want everything I see scoped to my Workspace, so that no other customer can ever access my surveys or responses. [tested](internal/http/surveys_test.go) — resource-level denial across every survey route, plus per-user separation in [workspace_test.go](internal/http/workspace_test.go)
5. As a user, I want to delete my account, so that my personal data leaves the system through the soft-delete and purge pipeline. [tested](internal/http/account_test.go) — soft-delete half; purge completes at M8-T2

### Building surveys

6. As a survey creator, I want to create a Survey with a title and choose at creation whether it is an Anonymous Survey or an Invited Survey, so that the privacy promise is fixed before anyone answers. [tested](internal/http/surveys_test.go)
7. As a survey creator, I want the anonymity choice to be permanently immutable, so that no later edit can betray what respondents were promised. [tested](internal/store/immutability_test.go) — enforced by a database trigger, not only by application code
8. As a survey creator, I want to build a Draft mixing ten question types (long text, short text, single choice, multiple choice, rating scale, NPS, yes/no, dropdown, date, number), so that each question gets its most suitable format. [tested](internal/http/surveys_test.go)
9. As a survey creator, I want every save of my Draft recorded as a Draft Revision, so that no editing work is ever lost. [tested](internal/store/immutability_test.go)
10. As a survey editor, I want an Audit Log of who changed what and when, so that collaboration is accountable. [tested](internal/http/surveys_test.go)
11. As a survey creator, I want to publish my Draft as an immutable Survey Version, so that what respondents saw can never be silently altered. [tested](internal/store/immutability_test.go)
12. As a survey creator, I want to keep editing after publishing — my changes accumulate in the Draft while the published version stays live — and publish again when ready, so that improving a live survey is safe and ordinary. [tested](internal/http/surveys_test.go)
13. As a survey creator, I want reworded questions to keep their Question Identity across versions, so that results stay comparable over time (responses stay pinned to the version answered; aggregation happens at read time — nothing is copied or migrated). [tested](internal/store/immutability_test.go)
14. As a survey creator, I want each survey to show its Survey Status — Draft, Open, or Closed — so that I always know what respondents can do. [tested](internal/domain/survey_test.go)
15. As a survey creator, I want to close an Open survey (manually or by Close Date) and reopen a Closed one, so that fieldwork windows are under my control. [tested](internal/http/surveys_test.go)
16. As a survey creator, I want a dashboard listing my Workspace's surveys with their status, so that I can find and manage them. [tested](internal/http/surveys_test.go)
17. As a survey creator, I want to preview my survey exactly as respondents will see it — before and after publishing — so that I never send something I haven't seen. [tested](internal/http/respond_test.go) — same template as the live renderer; the preview form posts to a handler with no write path
18. As a survey creator, I want the survey to render well on phones, tablets and desktops, so that respondents can answer on any device. [tested](e2e/tests/smoke.spec.ts) — every e2e test runs at all three widths, with a no-horizontal-overflow check

### AI-assisted creation

19. As a survey creator, I want to describe my goal in a prompt and watch AI-generated draft questions stream in, so that I start from a solid draft instead of a blank page. [tested](internal/http/generate_test.go) — streamed over a WebSocket, with the same operation available as a plain form post for JS-free use ([e2e](e2e/tests/generate.spec.ts))
20. As a survey creator, I want generated questions to arrive as editable Draft content restricted to the supported types, so that AI output is never a special case. [tested](internal/http/generate_test.go) — invalid or invented types are dropped and counted, never silently accepted; what lands is an ordinary Draft Revision
21. As a workspace owner, I want AI usage counted against a daily workspace quota, so that one enthusiastic teammate cannot burn the budget. [tested](internal/ai/meter_test.go) — quota and breaker logic; the SQL sums in [store](internal/store/aiusage_test.go)

### Localization and translation

22. As a survey creator, I want to write questions once and have AI draft translations into the languages I choose, so that respondents can answer in their mother tongue. [tested](internal/http/localizations_test.go)
23. As a survey creator, I want to review and edit every machine translation before publishing, so that nothing goes out in my name that I couldn't read. [tested](internal/http/localizations_test.go) — publishing is refused while anything is unreviewed, and rewording a question un-reviews its translation
24. As a survey creator, I want Localizations frozen into the published Survey Version, so that what a respondent saw in their language is as immutable as everything else. [tested](internal/http/localizations_test.go) — frozen in the publish transaction and guarded by the same trigger as questions
25. As a respondent, I want to pick my language (with my browser's language suggested), so that the survey speaks to me — without my choice being stored anywhere. [tested](internal/http/localizations_test.go) — the choice travels in the URL, on the form's own address too, so a form sent back for correction is still in the chosen language; no cookie, no column, and the browser's preference is suggested rather than applied. Personal links offer the same choice, and an option picked in a translation is stored as the option the creator wrote
26. As a survey creator, I want text answers and Transcripts translated on demand into my workspace language, with the original always preserved and viewable, so that I can read a global audience without losing what they actually said. [tested](internal/http/localizations_test.go) — cached per answer and language, so a second run costs nothing
27. As a survey creator, I want machine-translated content clearly marked, so that I never mistake AI translation for the respondent's own words. [tested](internal/http/localizations_test.go) — labelled with the model, shown beneath the original, and absent from the CSV export, which carries what was actually said

### Answering — the respondent experience

28. As a respondent, I want to open a share link and answer one question at a time with a progress indicator, so that the survey feels effortless. [tested](internal/http/respond_test.go) — and the paged JS flow end to end in [the e2e suite](e2e/tests/smoke.spec.ts)
29. As a respondent, I want to answer with only a keyboard, with a screen reader, or with JavaScript disabled, so that accessibility is never the price of polish. [tested](internal/http/respond_test.go) — plus a JavaScript-disabled browser run and axe-core scans in [the e2e suite](e2e/tests/smoke.spec.ts). Every answer control, and every field in the editor and the translation page, has autofill off, with the password manager opt outs on text fields; sign in and account fields keep theirs [tested](internal/http/autocomplete_test.go)
30. As a respondent, I want the survey page to load no third-party scripts, so that answering doesn't expose me to trackers. [tested](internal/http/respond_test.go) — every URL on the page is checked first-party
31. As a respondent, I want a closed survey to tell me clearly it no longer accepts responses, so that I'm not left guessing. [tested](internal/http/respond_test.go)
32. As a respondent mid-fill when a new version is published, I want my submission to count against the version I was actually shown, so that my answers are never misattributed. [tested](internal/http/respond_test.go)

### Answering — voice

33. As a respondent, I want to answer text questions by speaking, so that I can give rich answers without typing. [tested](internal/http/voice_test.go) — and the whole path in a real browser with a microphone in [the e2e suite](e2e/tests/voice.spec.ts)
34. As a respondent, I want my speech recognized locally in the browser when that is verifiably possible, so that my voice doesn't leave my device unnecessarily. [tested](e2e/tests/voice.spec.ts) — the detector refuses every browser that cannot *prove* on-device recognition; the decision table is [docs/voice-support.md](docs/voice-support.md)
35. As a respondent whose browser can't do local recognition, I want my audio streamed to Earful's EU transcription and immediately discarded, so that speaking stays possible without my voice being kept. [tested](internal/http/voice_test.go) — plus [internal/voice](internal/voice/voice_test.go), where a build-time test fails if the one package holding audio ever gains a way to write it anywhere
36. As a respondent, I want to see and edit the Transcript before it becomes my Answer, so that the record says what I meant. [tested](internal/http/voice_test.go) — transcribing stores nothing; the answer is the text the respondent submits
37. As a respondent, I want a clear consent moment before first microphone use stating that my voice is never stored, so that I can decide informed. [tested](e2e/tests/voice.spec.ts) — including an axe scan of the dialog
38. As a respondent, I want typing always available as an alternative, so that voice is a convenience, never a requirement. [tested](internal/http/voice_test.go) — the mic is built by JavaScript, so a browser that cannot record never renders one
39. As a respondent who exceeds the voice quota, I want a graceful fallback to typing with a clear message, so that I can still finish. [tested](internal/http/voice_test.go)
40. As a respondent answering in my chosen language, I want speech recognition to use that language, so that my words are transcribed correctly. [tested](internal/http/voice_test.go) — the hint reaches the provider; the respondent-facing language picker arrives with M11-T1. The language is the language of the questions, whatever the buttons are in: the survey's where they chose one, and none where they read it as written, whose language nothing records, so the server works out what was said; sent when a take begins ([the e2e suite](e2e/tests/language.spec.ts) reads it off the socket)

### Anonymous surveys

41. As an anonymous respondent, I want no email, IP address, or user-agent stored with my Response, so that "anonymous" means what it says. [tested](internal/http/respond_test.go) — and structurally: the schema has no such columns on the response path (`db/migrations/00004_responses.sql`), so the promise cannot be broken by a careless query
42. As an anonymous respondent, I want to pass a lightweight in-page challenge (ALTCHA) rather than a third-party CAPTCHA, so that proving I'm human doesn't identify me. [tested](internal/http/antibot_test.go) — solved invisibly by a first-party solver; forged and replayed solutions refused; no-JS respondents remain able to answer under a tighter rate bucket plus a min-fill-time check
43. As a survey creator, I want accidental double-submits softly deduplicated, so that my results aren't polluted by twitchy fingers (accepting that anyone with the link can answer repeatedly — the stated trade-off of anonymity). [tested](internal/http/antibot_test.go)

### Invited surveys

44. As a survey creator, I want to import participant emails by paste/CSV with duplicates removed, so that building the audience is quick. [tested](internal/http/participants_test.go)
45. As a Participant, I want a unique personal link by email, so that answering requires no account. [tested](internal/http/participants_test.go) — 256-bit tokens, stored hashed, minted at send time
46. As a survey creator, I want invites drip-sent under a per-workspace hourly cap, so that deliverability and reputation are protected. [tested](internal/http/participants_test.go) — 203 imported → 200 sent → next hour → 3 sent
47. As a Participant, I want exactly one submission tied to my link, with an "already submitted" page afterwards, so that results are one-per-person. [tested](internal/http/participants_test.go) — enforced by a partial unique index, not a code path
48. As a survey creator, I want bounced or complaining addresses automatically suppressed from future sending, so that we never spam. [tested](internal/http/participants_test.go) — suppression is global by address, across surveys
49. As a survey creator, I want each Response linked to the Participant's email in an Invited Survey, so that I know who answered what. [tested](internal/http/participants_test.go)

### Results, insights and export

50. As a survey creator, I want results aggregated across all Survey Versions by Question Identity, with wording changes labelled per version, so that editing never hides or distorts history. [tested](internal/http/results_test.go)
51. As a survey creator, I want distributions for choice/rating/NPS/yes-no questions and a transcript list for text questions, so that I can read the story at a glance. [tested](internal/http/results_test.go)
52. As a survey creator, I want an on-demand Insight Summary — themes, patterns, and representative quotes across all responses — so that hundreds of answers become a story in minutes. [tested](internal/http/insights_test.go) — one run spans every version, and prompts never carry participant identity
53. As a survey creator, I want Insight Summaries clearly labelled as AI-generated with model and timestamp, so that I never mistake analysis for data. [tested](internal/http/insights_test.go) — in the UI, in the workspace export, and as a labelled sidecar file in the archive
54. As a survey creator, I want insight runs cached until new responses arrive and counted against my quota, so that curiosity doesn't torch the budget. [tested](internal/http/insights_test.go) — a re-run with nothing new never reaches the provider, and a summary older than the answers says so
55. As a survey creator, I want survey stats — starts, completions, completion rate, average duration, drop-off per question — so that I can improve my surveys. [tested](internal/http/stats_test.go) — with one honest limit: "where answers stop" is derived from submitted responses, since true abandonment would need a per-question beacon on respondent pages (see the M7-T4 note). Since issue #2 the flow counters carry a day (ADR-0012), so the stats page shows them over a date range with a trend chart, and the duration figure is a median [tested](internal/http/statspage_test.go), and the average number of submissions per day over the range, on the page and in the stats CSV [tested](internal/http/statspage_test.go)
56. As a survey creator, I want audience aggregates (browser family, device class, country) that are never linkable to any individual response, so that I understand my audience without betraying anyone. [tested](internal/http/stats_test.go) — including the ADR-0009 unlinkability guard, which fails the build if any query touches the counters and responses together
57. As an anonymous respondent, I want aggregate buckets suppressed below five observations, so that small samples can't single me out. [tested](internal/http/stats_test.go)
58. As a survey creator, I want all responses in a tabular view and exportable as CSV that opens cleanly and safely in spreadsheet tools, so that analysis can continue elsewhere. [tested](internal/http/results_test.go) — a respondent's `=cmd|…` payload exports as text, not as a formula
59. As a workspace owner, I want a one-click full Workspace export (documented JSON + CSVs, async, expiring download), so that I can leave for a self-hosted instance at any time — the trust promise made real. [tested](internal/http/export_workspace_test.go) — the archive round-trips against [the documented format](docs/export-format.md)

### Data lifecycle and trust

60. As a survey creator, I want deleting a survey or response to be a soft-delete restorable by support for 30 days, so that mistakes aren't catastrophic. [tested](internal/purge/purge_test.go) — surveys and responses both; a survey deleted 29 days ago is still restorable, and on day 31 it is gone with everything under it
61. As an operator, I want a purge job that hard-deletes soft-deleted data older than 30 days, expires stale tokens, and trims the abuse log — runnable by hand locally and on a schedule in production, so that retention promises are kept mechanically. [tested](internal/purge/purge_test.go) — including idempotency and a dry run that changes nothing while reporting the real numbers
62. As a data subject, I want an erasure fast-path that support can trigger immediately (skipping the 30-day wait), so that GDPR requests complete within 24 hours. [tested](internal/http/adminerasure_test.go) — two steps (look up, then confirm), support-only, and honest that anonymous responses are unerasable because they hold nothing personal
63. As a respondent, I want the survey landing page to disclose who the controller is, whether the survey is anonymous, and how voice is processed, so that I understand before answering. [tested](internal/http/trust_test.go) — the voice sentence appears only where voice is actually on offer
64. As a privacy-conscious visitor, I want a public trust page listing processors, the no-recordings promise, EU hosting, and the leave-anytime export, so that I can verify the claims. [tested](internal/http/trust_test.go) — including the caveats (CLOUD Act, 30-day backup window) and a processor list that names only the companies the instance actually uses. The page is a Document (story 82), and what it says about the instance is supplied by the instance

### Operations (Earful as a service)

65. As an operator, I want staging and production in separate GCP projects with scale-to-zero staging, so that isolation is real and idle costs are ~zero.
66. As an operator, I want a €100/month operating target (alerts at €50/€80/€100) and a €200 hard cap, so that pre-revenue costs stay collaborator-approved and surprises are impossible.
67. As an operator, I want a global daily AI budget breaker that disables AI endpoints and alerts me when tripped, so that abuse can't bankrupt the product. [tested](internal/ai/meter_test.go) — trips for everyone at once and emits the Error-level alert line (a monitoring channel attaches at M9-T2)
68. As an operator, I want per-IP and per-survey rate limits, honeypot fields, and noindex on respondent pages, so that bots and LLM scrapers get nothing cheap. [tested](internal/http/antibot_test.go) — plus [noindex/robots](internal/http/respond_test.go)
69. As an operator, I want daily Cloud SQL exports into a retention-locked immutable bucket with a self-managing 30-day rolling window, so that ransomware with full app-project access still cannot destroy backups.
70. As an operator, I want PITR backups, uptime checks, error/latency alerts, and a drilled runbook (deploy, rollback, restore, erasure, breaker trip, breach basics), so that one person can run this calmly.
71. As an operator, I want first-party founder metrics — signups, surveys created/published, responses, completion rates, AI cost — from our own database with nothing added to respondent pages, so that I can steer the product without third-party analytics. [tested](internal/http/adminmetrics_test.go) — definitions and caveats in [docs/metrics.md](docs/metrics.md)
72. As an operator, I want an automated smoke test (create → share → answer → results) to run against staging after every deploy and gate promotion, so that the core journey can never silently break. [partially tested](e2e/tests/smoke.spec.ts) — the smoke test exists and runs in CI against the compose stack; pointing it at staging (`E2E_BASE_URL`) and gating deploys is M1-T3, cloud milestone

### Self-hosting

73. As a self-hoster, I want `docker compose --profile app up` to give me the full core loop (app + Postgres + local email catcher), so that adoption takes minutes.
74. As a self-hoster, I want AI features to work against ollama/llamafile via configuration or degrade gracefully when absent, so that no Google dependency is required. [tested](internal/ai/ai_test.go) — streaming verified against a real llamafile (opt-in integration test); unconfigured capabilities answer ErrUnsupported and the product treats them as absent features
75. As a self-hoster, I want magic-link auth over my own SMTP and optional Google OIDC, so that login works on my infrastructure.
76. As a self-hoster, I want the workspace export format documented as a stable contract, so that migrating into my instance is a solved problem (import tool: first post-MVP ticket). [tested](internal/http/export_workspace_test.go) — [docs/export-format.md](docs/export-format.md) is the contract; the test decodes a real archive into the documented types

### Private beta (decided 2026-07-24)

The SaaS is invite-only, and the account loop — deliberately — works with
zero emails sent: it was designed before any email infrastructure
existed, and stays email-free even now that live Brevo (M4-T6, closed
later the same day) can send. Codes are the gate AND the credential.

Turning the gate off is a manual decision with no date on it, and no
ticket depends on it: `BETA_MODE` is one environment variable, and the
software works either way. Whenever it happens, `/login` should keep
offering the password form beside the magic-link one — every account
created during the beta holds a password and, unless it also has a
Google identity, nothing else.

77. As the founder, I want account creation gated by one-shot secret invite codes from a list I control — minted by CLI or from an admin page only super admins can even see, labeled, revocable, and marked used the moment they create an account — so that only people I've invited can enter the private beta. [tested](internal/http/beta_test.go)
78. As a private-beta user, I want to create my account with an invite code and a password, sign in with email+password from then on, and change my email later (re-proving my password), with no email ever sent, so that I can use Earful before the email infrastructure exists. [tested](internal/http/beta_test.go)

### Draft answers survive a refresh (post-MVP)

A respondent part-way through a survey who reloads the page — or whose
phone browser evicts the tab — loses everything entered so far. The cost
falls hardest on the answers this product exists to collect: a long
dictated answer is both the most expensive to lose and the most tedious
to repeat.

79. As a respondent, I want the answers I have not submitted yet to survive a page reload, so that a stray refresh, a phone switching apps, or a tab restored an hour later does not cost me everything I have said.

**The draft never leaves the device.** It is kept in the browser's own
storage, not on the server, and that is a product decision rather than a
convenience: a server-side draft would need something to key it by, and
for an anonymous respondent the only candidates are a cookie or a
fingerprint — which is precisely the identification ADR-0003 refuses.
Local storage preserves the work without identifying the respondent, and
adds nothing to the processor table on `/trust` because no processor is
involved.

Consequences that make it honest rather than merely convenient:

- **Cleared the moment the response is submitted.** A draft that
  outlives its purpose is just an answer sitting on a shared computer.
- **Expires on its own** (24 hours) and is **scoped to the survey
  version**, so a republished survey never restores answers to questions
  that have since changed.
- **Never stores the security fields** — the render timestamp, the
  proof-of-work solution, the CSRF token, the honeypot. Restoring those
  would either break the anti-abuse checks or defeat them.
- **Stated in the respondent disclosure**, next to what happens to the
  answers themselves. "Your voice is never stored" is a statement about
  the service's servers; a respondent using a shared device needs to know
  that an unsubmitted draft remains on that device until submission or
  expiry.
- **An enhancement, like everything else in `web/static/js`**: with
  JavaScript off there is no draft, and the form still works.

### Earlier versions of an answer (post-MVP)

Dictation changes an answer in large strokes: a take lands a paragraph,
Reset removes one. A stroke made by mistake has no undo, because a value
set from script is outside the browser's own history.

81. As a respondent, I want to see what my answer said a moment ago and put it back, so that a Reset pressed by mistake, or a take that went wrong, does not cost me what I had.

- **One version per five seconds of editing**, taken at the end of the
  window, and listed with the time it was taken.
- **A large deletion first records what it is about to remove.** Without
  this, a Reset four seconds after the last version would erase exactly
  the text the history exists to keep.
- **Under the draft's rules**, because it is the draft's kind of data:
  in the browser and nowhere else, scoped to the survey version, expired
  after 24 hours, cleared on submit. Nothing is sent, so nothing is added
  to the processor table on `/trust`.
- **Offered where dictation is**, as a small *Previous versions* link
  under the answer, shown once there is a version to look at.
- **The respondent can clear it.** *Clear all* drops every version of
  that answer and leaves the answer itself alone, so a history on a
  shared device need not wait for submission or expiry.

### The interface in the reader's language (post-MVP)

83. As a respondent, I want the buttons, hints and messages around a survey to be in my language, so that I am not answering Spanish questions with English buttons — and without what language I read being stored anywhere. [tested](internal/http/language_served_test.go) — the survey language in the address where the interface is written in it, then the browser's, then English; no cookie is set and the switcher's is not read; the whole journey in Spanish in [the e2e suite](e2e/tests/language.spec.ts)
84. As a creator, or anyone reading a page that is not a survey, I want to choose the language of the interface and have the choice remembered, so that a shared or borrowed browser does not decide it for me. [tested](internal/http/language_served_test.go) — a form at the foot of the page that works without a script; the choice is kept in the `interface_lang` cookie, and outranks the browser
85. As whoever keeps the wording, I want every sentence the application says in one file per language, so that I can rewrite a sentence or translate one without reading a template. [tested](internal/uitext/guard_test.go) — the build fails on a name with no message, a message nothing uses, wording left in a template or a script, and a translation that differs from its source in placeholders, markup or forms

| Who is asking | Read, in order |
|---|---|
| A respondent (`/s/`, `/p/`) | `?lang=`, where the interface is written in that language; `Accept-Language`; English |
| A reader of a Document | `?lang=`; the `interface_lang` cookie; `Accept-Language`; English |
| Anyone else | the `interface_lang` cookie; `Accept-Language`; English |

The interface is written in English and Spanish. What is not worded for
a reader stays as it was: what is sent to a model, what is written to
the log, and the columns of an export, which are read by programs.

### Documents (post-MVP)

82. As a reader of the trust page, the terms or a help page, I want to see when it last changed and take a copy of it as Markdown, so that I can keep what I was told and show it to someone else. [tested](internal/http/documents_test.go) — and the copy button, the link that stands in for it without JavaScript, and the axe scan in [the e2e suite](e2e/tests/documents.spec.ts)

A Document is a Markdown file in `web/pages`, and its place there is its
address. Its front matter is the whole of its record:

| Line | Says | Written by |
|---|---|---|
| `title` | The heading of the page | its writer |
| `short_title` | The window's title, where the heading is too long for one | its writer |
| `sections: cards` | Each section is drawn in a card | its writer |
| `draft: true` | Not ready: served only in development | its writer |
| `hash` | Which text this is | `make pages` |
| `last_update` | The day that text replaced the last | `make pages` |
| `source_hash` | On a translation, which English it was made from | `make text-accept` |

### A survey to start from (post-MVP)

A new workspace showed its owner an empty list. What a respondent sees,
which is what a creator is there to make, was a title, a question, a
publish and a link away.

86. As a new creator, I want my Workspace to begin with a published survey that is mine to change, so that I see what my respondents will see before I have written a question. [tested](internal/http/starter_test.go) — by both paths that create a Workspace; what the survey says is tested [where it is written](internal/starter/starter_test.go)

### A look of its own (post-MVP)

Earful was drawn in the browser's defaults: its system face, a purple
accent, and the word "Earful" in bold for a mark. For a product whose
promise is that people answer, the page asking them is the product.

87. As a creator or a respondent, I want every page to look and speak as one calm, professional product, in either light or dark, so that I trust it with my questions and my answers. [tested](internal/uitext/dash_test.go) — the wording carries no dashes in any language, a stylesheet never styles a hook a script or test relies on ([tested](web/static/static_test.go)), and every page is axe-clean in both themes ([tested](e2e/tests/smoke.spec.ts)); how it looks is reviewed page by page with `make gallery` against [the style guide](docs/style-guide.md)
88. As anyone using Earful, I want a Help page in my language one link away from every page, so that I can find out how to make, share, read or answer a survey without asking. [tested](internal/http/documents_test.go)
89. As a creator, I want to name my Workspace, so that respondents read who is asking in words I chose, in my language. [tested](internal/http/workspace_test.go)
90. As a creator, I want a survey's results as an Excel workbook with the same columns and rows as the CSV, numbers as numeric cells and text defused against formula injection, so that I can open them in a spreadsheet without an import step. [tested](internal/http/export_xlsx_test.go)
91. As the operator of an instance, I want each workspace's daily AI allowance set by a tier (low normal, normal or high) that a super admin can change, so that a heavy customer can have more and a trial less without changing the cap for everyone. [tested](internal/ai/meter_test.go) — each tier's cap, and the € breaker outranking every tier; the super-admin control, its authorization and the same-day lift in [the HTTP suite](internal/http/adminaitiers_test.go); the caps' configuration in [config](internal/config/config_test.go)
92. As a creator, I want to switch a preview between one question at a time and every question on one page, so that I can read a long survey without stepping through it; respondents always get the paged form. [tested](internal/http/respond_test.go) — and in [the e2e suite](e2e/tests/preview.spec.ts)
93. As a survey creator, I want to ask for a date, such as when someone visited, answered with the browser's own date control and counted per day in the results, so that answers about when can be sorted and compared. [tested](internal/http/date_test.go)
94. As an account holder closing my account, I want to tick "Send me a copy of my data" and receive an emailed link to my whole workspace archive that works for 7 days without signing in, or an email saying why no copy could be made, so that closing does not cost me my data. [tested](internal/http/export_closure_test.go) — the token is stored only as SHA-256, one per workspace, and the nightly purge clears the archive after day 7 [tested](internal/purge/purge_test.go)
95. As a survey creator, I want to start a new survey from a description, with AI proposing the title and drafting the questions, so that I begin from a draft rather than a blank form. [tested](internal/http/create_from_prompt_test.go) — the same metered generation as the editor's panel; a refused or silent run creates nothing and hands the form back as typed; the drafted questions are ordinary Draft content, edited like story 20's ([e2e](e2e/tests/generate.spec.ts), with and without JavaScript)
96. As anyone reading Earful, a respondent included, I want to choose a light or a dark theme, or follow my system, so that the page suits my eyes whatever my system says. [tested](internal/http/theme_test.go) — a form at the foot of every page that works without a script; the choice is kept in the `theme` cookie, drawn by the server as `data-theme` on `<html>`, set only when chosen and removed by following the system; the dark tokens are the same however dark is reached ([tested](web/static/static_test.go)); applied without a reload and kept across one in [the e2e suite](e2e/tests/theme.spec.ts)
97. As a survey creator, I want to ask for a whole number between a lowest and highest answer I set, such as how many people someone came with, answered with the browser's own number field and read in the results as its average, median, lowest and highest, so that counts are not squeezed into a rating scale. [tested](internal/http/number_test.go) — limits in [the domain suite](internal/domain/number_test.go); typed digits reach the field, not the next question's scale, in [the e2e suite](e2e/tests/number.spec.ts)

**The Starter Survey is its owner's survey, not Earful's.** It is
written once, with the Workspace, and from then on nothing treats it
differently from a survey its owner made: its answers arrive in that
Workspace and nowhere else, and rewording, closing or deleting it is
the owner's to do. Earful asks the same five questions of its own
creators from its own Workspace.

| # | Asks | Answered with | Required |
|---|---|---|---|
| 1 | How the respondent heard about Earful | words, which can be spoken | no |
| 2 | How satisfied they are so far | a scale of 1 to 5 | yes |
| 3 | How often they expect to run a survey | one of four choices | no |
| 4 | What is missing or should work differently | words, which can be spoken | no |
| 5 | How likely they are to recommend Earful | 0 to 10 | yes |

Consequences:

- The first question is one that can be spoken, since a respondent
  meets one question at a time and dictation is what they should meet
  first. It does not say so: an instance without a transcriber offers
  no microphone.
- The survey is English with a Spanish Localization, whoever signs up,
  so its address means one survey. Rewording a question therefore
  calls for the Spanish to be read again before publishing (story 23),
  or for the language to be removed.
- The dashboard says what the survey is, on its card, until its owner
  publishes a version of their own.
- A deleted Starter Survey does not come back. `earful starter-survey
  add` gives a Workspace one where its owner asks, and to a Workspace
  made before this.
- Workspaces that already exist are not given one.

### Answering from the keyboard (post-MVP)

80. As a respondent, I want to answer the whole survey from the keyboard, with the key for each answer shown next to it, so that I can move as fast as I think instead of aiming a mouse at every option.

The scheme follows the convention established by comparable products —
letters for choices, digits for scales — rather than inventing one:

| Where | Key | Does |
|---|---|---|
| Single / multiple choice, dropdown | `A` `B` `C` … | Select, or toggle for multiple choice |
| Rating & NPS | `0`–`9`, buffered so `1` `0` means ten | Pick that point |
| Yes / No | `Y` `N` | Pick |
| Text question offering voice | `Space`, held | Record while held; transcribe on release |
| Text question offering voice | `⇧Space` | Start, then stop recording |
| In a text field | `Esc` | Leave the field, so that `↵` moves on; the answer is untouched |
| Text question offering voice, or while recording or transcribing | `⇧Esc` | Clear the answer; a take is dropped, not transcribed |
| Not in a text field | `↵` / `⇧↵` | Next / Back |
| In a textarea | `↵`, `⇧↵` | Newline, untouched |
| In a textarea | `⌘↵` / `Ctrl↵` | Next |
| Last question | `↵`, or `⌘↵` / `Ctrl↵` from a textarea, or `Esc` then `↵` | Submit |

**Letters for choices, digits for scales** is the load-bearing decision.
Assigning digits to options would collide with rating questions, where a
digit already names a value, and most surveys mix the two question types.

**Enter remains a newline inside a textarea**, which departs from that
convention. Long answers here are frequently dictated and then edited, so
advancing on Enter would lose a respondent's place mid-paragraph;
`Cmd/Ctrl+Enter` advances instead.

**Esc leaves a text field**, which gives a respondent who has finished
typing a plain way on: `Esc`, then `↵`. It works on every text question,
with or without voice, and changes nothing in the answer.

**The buttons that move say which keys**, in the order to press them:
Back, Next, and Submit answers on the last question.

- **A key is drawn as a keycap**, the way shortcuts are written wherever
  they are documented, and one cap is one key. Keys pressed together
  are joined by `+`; keys pressed in turn, by "then".
- **The symbol, then the name**: `↵ Enter`, `⇧ Shift`. The symbol is a
  picture of a key not every keyboard has printed on it, so it never
  stands alone. Keys with no symbol in common use are named: `ESC`,
  `Space`.
- Next and Submit read `↵ Enter`; Back reads `⇧ Shift` + `↵ Enter`.
- While a textarea has focus each is preceded by `ESC` "then", because
  there Enter is a newline, with Shift or without.
- What the hint says is what happens: on the last question `Esc` then
  `↵` submits.
- The words around a key are plain text: "Hold" `Space`.
- Reset reads `⇧ Shift` + `ESC`.
- **Back, Next and Submit are the largest controls on the page**, and
  each points the way it goes: an arrow leading the word on Back, one
  following it on Next, a paper plane following Submit. On the last
  question Submit takes Next's place in the row, beside Back.

**Shift+Esc clears the answer**, in one press. It is a chord rather than
a second `Esc` because `Esc` has a use of its own, and a key that leaves
the field on one press must not destroy the answer on the next: however
often `Esc` is pressed, nothing is cleared.

- It works from the field, from the dictation controls, and with nothing
  focused, which is where `Esc` leaves a respondent.
- A take that is recording or being transcribed is dropped with it.
- Clearing puts focus back in the field, empty and ready to be typed
  into, and what was cleared is kept as a previous version (story 81).
- Some browsers bind `⇧Esc` to a process or task manager of their own.
  The page claims the key when it acts on it, and the *Reset* button
  does the same thing for a respondent whose browser gets there first.

Consequences:

- **Hints appear only where they can be used**: the shortcuts require
  JavaScript, so hints render only once it has upgraded the form, and only
  on devices reporting a keyboard. Displaying a shortcut that cannot be
  pressed misleads the respondent.
- **Hints are `aria-hidden`.** Without it an option's accessible name
  becomes "B Pro" rather than "Pro", and every choice is announced
  incorrectly.
- **Nothing native is replaced.** Tab, arrow keys within a radio group and
  Space to toggle keep their behaviour, since assistive technology relies
  on them.
- **Dropdowns render as a lettered list** rather than a `<select>`: a
  browser draws its own option popup and there is nowhere in it to put a
  hint. The submitted field name and values are unchanged. The cost is
  that a dropdown and a single-choice question now look the same to a
  respondent, and the distinction survives only in the editor.

## Implementation Decisions

All load-bearing decisions are recorded as ADRs; the spec inherits them:

- **Versioning (ADR-0001):** Draft → publish → immutable Survey Version; Responses pin to the version served, never copied ("response migration" in stakeholder language = this read-time aggregation, not data movement); results aggregate by Question Identity. Immutability enforced at store layer and DB triggers.
- **Ownership (ADR-0002):** Workspaces own surveys; personal workspace auto-created; MVP hard-codes sole-member access; the workspace is the future billing and DPA boundary.
- **Anonymity (ADR-0003, amended by 0009):** anonymous Responses carry no email/IP/UA — no such columns exist on the response path; IPs only in a separate short-retention abuse log with no join path to responses; `is_anonymous` trigger-guarded immutable.
- **Voice (ADR-0004):** transcript-only; local browser recognition only when verifiably local, else PCM → WebSocket → Vertex AI Gemini on the EU multi-region endpoint (ADR-0013); audio in memory only, never persisted or logged; respondent reviews the Transcript; the respondent's chosen language drives recognition locale and transcription hints. Per-survey opt-in audio retention was proposed (collaborator Q4) and deliberately kept out — it stays a backlog decision requiring its own ADR and DPA language.
- **Email (ADR-0005):** Brevo behind a two-method `Sender` interface; SMTP for self-hosters; SPF/DKIM/DMARC on a dedicated subdomain; webhook-fed suppression; drip caps. CRM integration is out of scope; the interface is the future integration point.
- **Anti-abuse (ADR-0006):** ALTCHA in-app, first-party widget; zero third-party scripts on respondent pages (CI-enforced); honeypot; token-bucket rate limits; session-bound LLM tokens; per-workspace daily AI quotas plus a global daily € breaker.
- **Platform (ADR-0007):** Cloud Run, separate stg/pro projects, opentofu; one Go binary with `serve | purge | migrate` subcommands; purge cron = same binary as a scheduled Cloud Run job; WebSocket clients auto-reconnect.
- **Workspace export (ADR-0010):** the archive is built asynchronously and stored in Postgres, not object storage — one code path for the SaaS and for `docker compose up`, at the cost of a documented size cap.
- **AI residency (ADR-0011, amended by 0013):** every AI call is processed inside the EU, never at Vertex's global location, even when a newer model is available only there. The pin is Vertex's `eu` multi-region endpoint, whose documented guarantee is ML processing in EU member states; single EU regions carried no stronger written commitment. Model ids and the location are configuration; a newer EU-resident model is a tfvars change.
- **Backups (ADR-0008):** daily Cloud SQL Admin API exports to a retention-locked bucket in a separate backups project; lifecycle rule owns the rolling 30-day window; export credentials create-only.
- **Audience aggregates (ADR-0009):** survey stats (starts, completions, completion rate, drop-off per question position, average duration) plus audience aggregates (browser family, device class, country) exist only as survey-level counters with no join path to responses; country derived in-process from an embedded GeoIP database with the IP discarded in-request (no new processor); per-response duration is the only per-response addition; UI suppresses buckets with n < 5. The blessed list is exhaustive.
- **Survey Status:** Draft / Open / Closed is a derived, first-class status (never-published; published and accepting; not accepting). Manual close and reopen plus Close Date enforcement. Status is survey-level, like Close Date — not part of the versioned structure.
- **AI surface:** one `Provider` interface — `Generate`, `Transcribe`, `Translate`, `Analyze`, all streaming; Vertex implementation for stg/pro (model IDs are configuration; insights use the Pro tier, the rest Flash), OpenAI-compatible implementation for local dev; every call metered into quota accounting.
- **Localization model:** AI-drafted, creator-reviewed translations of a version's question set; mandatory review before publish; frozen into the immutable version (adding a language later = new version). Answer translations are creator-side, on-demand, cached, marked machine-translated, and never overwrite the original.
- **Insights model:** on-demand runs cached against a response watermark; output stored append-only, labelled with model + timestamp; prompts never include participant identity; included in exports as clearly-AI-generated content.
- **Stack:** Go stdlib HTTP + a-h/templ + hand-written CSS/JS with progressive enhancement (every form works without JS); pgx + sqlc + goose; coder/websocket; server-side sessions in Postgres with CSRF protection; strict self-only CSP, HSTS, Referrer-Policy no-referrer; slog JSON with scrubbing — no emails, tokens, transcripts, or answer content in logs.
- **Schema shape** (condensed; encodes pin-don't-copy, anonymity, and the new aggregate/localization decisions):

  ```
  workspaces ─< surveys(is_anonymous IMMUTABLE, close_at, closed_at, deleted_at)
                 ├─ survey_drafts(1:1) ─< draft_revisions(append-only)
                 ├─< survey_versions(immutable) ─< questions(question_identity_id)
                 │      └─< question_localizations(lang, frozen at publish)
                 └─< participants(email, token_hash, suppression state)
  responses(version_id, participant_id NULLABLE — NULL forever when anonymous, duration_secs)
     └─< answers ─< answer_translations(original always kept)
  insight_runs(append-only, AI-labelled) · survey_stats(unlinked counters only)
  ai_usage(metering) · abuse_log(no FK to responses) · suppressions
  ```

- **Deliberate non-decisions:** GDPR paperwork (DPA, privacy policy, RoPA, external counsel) tracked as the plan's gap list; EU AI Act additions arrive in a dedicated follow-up pass (transparency labelling of AI outputs is already normative here).

## Testing Decisions

- **One seam: the application edge.** Tests boot the real server in-process (real routing, middleware, templ rendering, WebSockets) against a real Postgres, and drive it exactly as a browser would. Assertions are on external behavior only: status codes, rendered HTML, streamed frames, rows a user could observe via the UI or export. No test reaches into internal packages.
- **Fakes only where the world ends:** the AI `Provider` (scripted streaming fake covering Generate, Transcribe, Translate, Analyze — including language-hint passthrough), the email `Sender` (capturing fake), the identity provider (`internal/oidctest` — a real OIDC issuer with fake identities, so discovery, code exchange and token verification all stay on the real code path), and an injectable clock (close dates, magic-link expiry, purge windows, insight cache watermarks).
- **Isolation is by unique data, not truncation** (decided M2-T1): tests share one database and each creates its own users and workspaces, so workspace scoping — the same mechanism that isolates real customers — isolates tests, and everything runs in parallel. The rule this imposes: never assert on global counts, only on data the test itself created.
- **One exception to the HTTP-only seam** (added M3): invariants enforced by database triggers — version and question immutability, append-only revisions, fixed anonymity — are tested by issuing the forbidden `UPDATE`/`DELETE` directly. A test confined to the application's own routes could not distinguish "the database refuses this" from "no handler happens to do it", and that distinction is the whole guarantee.
- **The purge subcommand is tested at the same level:** run against a seeded database, assert what survives, using the clock to time-travel.
- **Behavior contracts with dedicated tests:** version immutability (including frozen Localizations); anonymity (no identifying data retrievable for anonymous responses by any query the app can run); **aggregate unlinkability** (no app query associates a survey_stats counter with a response) and n<5 suppression; cross-workspace authorization denial; pin-don't-copy aggregation (the 50-v1/30-v2 scenario) now also spanning insight runs; mandatory-review gate on localization publish; original-preservation on answer translation; CSV injection safety; suppression honoring; quota and breaker trips; ALTCHA rejection of unsolved challenges.
- **Out-of-band checks:** axe-core scan of respondent pages; CI assertion that respondent pages reference only first-party origins; responsive rendering checked at phone/tablet/desktop widths; local speech feature-detection gets a JS unit test plus a documented manual browser matrix; a real end-to-end smoke test (create → share → answer → results) runs against staging after every deploy and gates promotion.
- **Automated tests never send audio to a real transcription model.** A test runner has no microphone, so anything it sends is machine-generated — a synthesized tone, or speech from a text-to-speech binary. Neither tells you whether transcription works, both cost money, and a loop of them arriving at a hosted speech API looks exactly like probing it: on 2026-07-25 Google suspended the staging project hours after Vertex was first enabled there, with a synthesized tone being the only thing in that window worth suspecting. So the browser suite runs voice against the scripted provider only, and refuses to run at all when pointed at a real one (`E2E_VOICE_MODE`, enforced by a skip in `voice.spec.ts` rather than by anyone remembering). Real transcription is proven by `internal/ai`'s opt-in integration test, which sends **real recorded speech, once, on purpose** (`testdata/jfk.wav`, public domain), and by a person speaking into an actual microphone. There is no synthesized audio anywhere in the repository, deliberately: the moment one exists, someone eventually points it at a real model. A model that takes voice is the one thing in this product that cannot be checked automatically end to end, and pretending otherwise is what got a project suspended.
- **Prior art:** none — greenfield. This spec's seam choice *is* the prior art; the first milestone establishes the harness every later ticket reuses.

### Test coverage log

Each numbered story above carries an inline `[tested](...)` link once it
is implemented and covered. M0 shipped no numbered story (it is pure
infrastructure); M2 covers stories 1–5. Full harness rationale, the
isolation model, and the Playwright MCP front-end verification steps: see
[docs/testing.md](docs/testing.md).

Infrastructure and cross-cutting behaviour covered so far:

- Application-edge harness (real handler, real Postgres, `httptest.Server`, fake clock, captured outbox) — `internal/apptest/apptest.go`
- Fake OIDC issuer (discovery, JWKS, RS256 ID tokens) — `internal/oidctest/oidctest.go`
- Config loading — `internal/config/config_test.go`
- Log scrubbing (attrs, nested groups, `Logger.With`, query-string redaction) — `internal/logging/scrub_test.go`
- Sessions, CSRF and cookie attributes: fixation impossible, server-side logout, `Secure` outside development, CSRF token matrix, cross-site rejection — `internal/http/session_test.go`
- Magic-link safety properties: scanner-prefetch does not consume, replay refused, expiry, per-email and per-IP limits, no account enumeration — `internal/http/auth_magic_test.go`
- Token entropy and at-rest hashing — `internal/auth/tokens_test.go`
- Rate limiter, clock, email senders — `internal/antibot/`, `internal/clock/`, `internal/email/`
- Database-enforced invariants, asserted by attempting the forbidden mutation in raw SQL (the one deliberate exception to the HTTP-only seam, since the point is that these hold against paths the application does not offer): published versions and questions reject UPDATE and DELETE; draft revisions are append-only; `is_anonymous` cannot be changed — `internal/store/immutability_test.go`
- Survey structure invariants: question validation per type, draft add/replace/remove/reorder, publish re-validation, Status derivation including the exact close instant — `internal/domain/survey_test.go`

## Out of Scope

Branching/conditional logic and the drag-and-drop flow editor (first backlog item, schema-shaping, deliberately not rushed; ADR-0020 proposed); ranking and rating-matrix question types; results sharing outside the workspace; brand preferences / creator profile (ADR-0018, ADR-0019 proposed); pictures in questions (ADR-0021 proposed); scheduled delivery of responses to a chosen destination (ADR-0022 proposed); framing a survey in another site (ADR-0017 proposed: a link and button snippet first, then a frame only from origins the creator lists); per-survey opt-in audio retention (consciously deferred; reopening it requires a new ADR, consent UX, and DPA language); survey improvement suggestions; AI images (Nano Banana 2) and sections; CRM integrations; workspace member-invite UI (schema supports it; MVP is sole-member); response editing after submit; workspace import tool (export format is the contract); Helm chart; billing/payments; the homepage repo's copy fix ("your voice is never stored") — required before launch but lives in TryEarful/homepage; GDPR legal documents (DPA template, privacy policy text, RoPA, external counsel) — the plan's gap list; EU AI Act companion additions — next pass; non-local browser speech recognition (deliberately never used, ADR-0004).

## Further Notes

- Budget: €100/month pre-revenue operating target (alerts €50/€80/€100), €200 hard cap; typical projection ≈ €85–190 with the expanded AI scope, so AI quotas start tight (plan, Appendix C).
- Execution order: M0–M8, then M10 (insights) and M11 (localization/translation), then M9 (launch last). Milestone numbers are stable ticket-import IDs, not a sequence.
- Anonymous multi-submission is an accepted trade-off, stated to creators in the UI, not a bug to fix later.
- Verify at build time: current Vertex model IDs and written no-training/EU-residency terms (owner + deadline + plan B tracked in the plan's gap list); ALTCHA Go library state; Brevo webhook formats; GeoIP database licensing.
- Vocabulary is normative per CONTEXT.md (Survey, Draft, Draft Revision, Survey Version, Question Identity, Response, Answer, Workspace, Participant, Respondent, Creator, Transcript, Close Date, Survey Status, Localization, Insight Summary, Audit Log). "Recording" deliberately does not appear except to say recordings do not exist.
- Milestone/ticket decomposition (M0–M11, 56 tickets) lives in PLAN.md; this spec is the what-and-why those tickets implement.
