---
status: ready-for-agent
source: grilling session 2026-07-12/15; updated 2026-07-19 from "Voice questionnaires app details 002" — see PLAN.md, CONTEXT.md, docs/adr/0001–0009
pending: EU AI Act additions (companion doc) — separate pass
---

# Earful MVP — Specification

## Problem Statement

Teams that need honest feedback don't trust closed survey SaaS with their respondents' data, and respondents don't finish surveys that make them type. Existing tools (Typeform et al.) are proprietary, US-hosted, and text-first: creators can't self-host or leave with their data, privacy-sensitive organizations can't adopt them, and open-text answers — the valuable ones — go unanswered because typing is friction. Researchers additionally drown in raw answers: reading hundreds of free-text replies to find themes is manual work, and language barriers shrink who they can ask. There is no open-source, EU-hosted, voice-first survey tool that people can pay for as a service *and* walk away from with their data.

## Solution

Earful: an open-source (AGPL-3.0) survey platform, hosted in the EU (europe-west4), where respondents can *speak* their answers — transcribed instantly, with the audio never stored — and creators build surveys with AI assistance. Creators run anonymous or invited surveys in eight question types, localize questions into their respondents' languages, read answers translated back into their own, and get AI Insight Summaries — themes, patterns, representative quotes — across the whole respondent population. Append-only versioning never destroys or misattributes a response; results aggregate across versions; survey stats and privacy-preserving audience aggregates close the improvement loop without tracking anyone. Creators can export their entire workspace at any time to move to a self-hosted instance. Trust is the product: no third-party scripts on respondent pages, strong anonymity guarantees, transparent processor list, immutable backups.

## User Stories

### Accounts, workspaces and access

1. As a survey creator, I want to log in with my Google account, so that I can start without creating another password.
2. As a survey creator, I want to log in by receiving a magic link by email, so that I can use Earful without a Google account.
3. As a survey creator, I want a personal Workspace created automatically on first login, so that I can create surveys immediately.
4. As a workspace member, I want everything I see scoped to my Workspace, so that no other customer can ever access my surveys or responses.
5. As a user, I want to delete my account, so that my personal data leaves the system through the soft-delete and purge pipeline.

### Building surveys

6. As a survey creator, I want to create a Survey with a title and choose at creation whether it is an Anonymous Survey or an Invited Survey, so that the privacy promise is fixed before anyone answers.
7. As a survey creator, I want the anonymity choice to be permanently immutable, so that no later edit can betray what respondents were promised.
8. As a survey creator, I want to build a Draft mixing eight question types (long text, short text, single choice, multiple choice, rating scale, NPS, yes/no, dropdown), so that each question gets its most suitable format.
9. As a survey creator, I want every save of my Draft recorded as a Draft Revision, so that no editing work is ever lost.
10. As a survey editor, I want an Audit Log of who changed what and when, so that collaboration is accountable.
11. As a survey creator, I want to publish my Draft as an immutable Survey Version, so that what respondents saw can never be silently altered.
12. As a survey creator, I want to keep editing after publishing — my changes accumulate in the Draft while the published version stays live — and publish again when ready, so that improving a live survey is safe and ordinary.
13. As a survey creator, I want reworded questions to keep their Question Identity across versions, so that results stay comparable over time (responses stay pinned to the version answered; aggregation happens at read time — nothing is copied or migrated).
14. As a survey creator, I want each survey to show its Survey Status — Draft, Open, or Closed — so that I always know what respondents can do.
15. As a survey creator, I want to close an Open survey (manually or by Close Date) and reopen a Closed one, so that fieldwork windows are under my control.
16. As a survey creator, I want a dashboard listing my Workspace's surveys with their status, so that I can find and manage them.
17. As a survey creator, I want to preview my survey exactly as respondents will see it — before and after publishing — so that I never send something I haven't seen.
18. As a survey creator, I want the survey to render well on phones, tablets and desktops, so that respondents can answer on any device.

### AI-assisted creation

19. As a survey creator, I want to describe my goal in a prompt and watch AI-generated draft questions stream in, so that I start from a solid draft instead of a blank page.
20. As a survey creator, I want generated questions to arrive as editable Draft content restricted to the supported types, so that AI output is never a special case.
21. As a workspace owner, I want AI usage counted against a daily workspace quota, so that one enthusiastic teammate cannot burn the budget.

### Localization and translation

22. As a survey creator, I want to write questions once and have AI draft translations into the languages I choose, so that respondents can answer in their mother tongue.
23. As a survey creator, I want to review and edit every machine translation before publishing, so that nothing goes out in my name that I couldn't read.
24. As a survey creator, I want Localizations frozen into the published Survey Version, so that what a respondent saw in their language is as immutable as everything else.
25. As a respondent, I want to pick my language (with my browser's language suggested), so that the survey speaks to me — without my choice being stored anywhere.
26. As a survey creator, I want text answers and Transcripts translated on demand into my workspace language, with the original always preserved and viewable, so that I can read a global audience without losing what they actually said.
27. As a survey creator, I want machine-translated content clearly marked, so that I never mistake AI translation for the respondent's own words.

### Answering — the respondent experience

28. As a respondent, I want to open a share link and answer one question at a time with a progress indicator, so that the survey feels effortless.
29. As a respondent, I want to answer with only a keyboard, with a screen reader, or with JavaScript disabled, so that accessibility is never the price of polish.
30. As a respondent, I want the survey page to load no third-party scripts, so that answering doesn't expose me to trackers.
31. As a respondent, I want a closed survey to tell me clearly it no longer accepts responses, so that I'm not left guessing.
32. As a respondent mid-fill when a new version is published, I want my submission to count against the version I was actually shown, so that my answers are never misattributed.

### Answering — voice

33. As a respondent, I want to answer text questions by speaking, so that I can give rich answers without typing.
34. As a respondent, I want my speech recognized locally in the browser when that is verifiably possible, so that my voice doesn't leave my device unnecessarily.
35. As a respondent whose browser can't do local recognition, I want my audio streamed to Earful's EU transcription and immediately discarded, so that speaking stays possible without my voice being kept.
36. As a respondent, I want to see and edit the Transcript before it becomes my Answer, so that the record says what I meant.
37. As a respondent, I want a clear consent moment before first microphone use stating that my voice is never stored, so that I can decide informed.
38. As a respondent, I want typing always available as an alternative, so that voice is a convenience, never a requirement.
39. As a respondent who exceeds the voice quota, I want a graceful fallback to typing with a clear message, so that I can still finish.
40. As a respondent answering in my chosen language, I want speech recognition to use that language, so that my words are transcribed correctly.

### Anonymous surveys

41. As an anonymous respondent, I want no email, IP address, or user-agent stored with my Response, so that "anonymous" means what it says.
42. As an anonymous respondent, I want to pass a lightweight in-page challenge (ALTCHA) rather than a third-party CAPTCHA, so that proving I'm human doesn't identify me.
43. As a survey creator, I want accidental double-submits softly deduplicated, so that my results aren't polluted by twitchy fingers (accepting that anyone with the link can answer repeatedly — the stated trade-off of anonymity).

### Invited surveys

44. As a survey creator, I want to import participant emails by paste/CSV with duplicates removed, so that building the audience is quick.
45. As a Participant, I want a unique personal link by email, so that answering requires no account.
46. As a survey creator, I want invites drip-sent under a per-workspace hourly cap, so that deliverability and reputation are protected.
47. As a Participant, I want exactly one submission tied to my link, with an "already submitted" page afterwards, so that results are one-per-person.
48. As a survey creator, I want bounced or complaining addresses automatically suppressed from future sending, so that we never spam.
49. As a survey creator, I want each Response linked to the Participant's email in an Invited Survey, so that I know who answered what.

### Results, insights and export

50. As a survey creator, I want results aggregated across all Survey Versions by Question Identity, with wording changes labelled per version, so that editing never hides or distorts history.
51. As a survey creator, I want distributions for choice/rating/NPS/yes-no questions and a transcript list for text questions, so that I can read the story at a glance.
52. As a survey creator, I want an on-demand Insight Summary — themes, patterns, and representative quotes across all responses — so that hundreds of answers become a story in minutes.
53. As a survey creator, I want Insight Summaries clearly labelled as AI-generated with model and timestamp, so that I never mistake analysis for data.
54. As a survey creator, I want insight runs cached until new responses arrive and counted against my quota, so that curiosity doesn't torch the budget.
55. As a survey creator, I want survey stats — starts, completions, completion rate, average duration, drop-off per question — so that I can improve my surveys.
56. As a survey creator, I want audience aggregates (browser family, device class, country) that are never linkable to any individual response, so that I understand my audience without betraying anyone.
57. As an anonymous respondent, I want aggregate buckets suppressed below five observations, so that small samples can't single me out.
58. As a survey creator, I want all responses in a tabular view and exportable as CSV that opens cleanly and safely in spreadsheet tools, so that analysis can continue elsewhere.
59. As a workspace owner, I want a one-click full Workspace export (documented JSON + CSVs, async, expiring download), so that I can leave for a self-hosted instance at any time — the trust promise made real.

### Data lifecycle and trust

60. As a survey creator, I want deleting a survey or response to be a soft-delete restorable by support for 30 days, so that mistakes aren't catastrophic.
61. As an operator, I want a purge job that hard-deletes soft-deleted data older than 30 days, expires stale tokens, and trims the abuse log — runnable by hand locally and on a schedule in production, so that retention promises are kept mechanically.
62. As a data subject, I want an erasure fast-path that support can trigger immediately (skipping the 30-day wait), so that GDPR requests complete within 24 hours.
63. As a respondent, I want the survey landing page to disclose who the controller is, whether the survey is anonymous, and how voice is processed, so that I understand before answering.
64. As a privacy-conscious visitor, I want a public trust page listing processors, the no-recordings promise, EU hosting, and the leave-anytime export, so that I can verify the claims.

### Operations (Earful as a service)

65. As an operator, I want staging and production in separate GCP projects with scale-to-zero staging, so that isolation is real and idle costs are ~zero.
66. As an operator, I want a €100/month operating target (alerts at €50/€80/€100) and a €200 hard cap, so that pre-revenue costs stay collaborator-approved and surprises are impossible.
67. As an operator, I want a global daily AI budget breaker that disables AI endpoints and alerts me when tripped, so that abuse can't bankrupt the product.
68. As an operator, I want per-IP and per-survey rate limits, honeypot fields, and noindex on respondent pages, so that bots and LLM scrapers get nothing cheap.
69. As an operator, I want daily Cloud SQL exports into a retention-locked immutable bucket with a self-managing 30-day rolling window, so that ransomware with full app-project access still cannot destroy backups.
70. As an operator, I want PITR backups, uptime checks, error/latency alerts, and a drilled runbook (deploy, rollback, restore, erasure, breaker trip, breach basics), so that one person can run this calmly.
71. As an operator, I want first-party founder metrics — signups, surveys created/published, responses, completion rates, AI cost — from our own database with nothing added to respondent pages, so that I can steer the product without third-party analytics.
72. As an operator, I want an automated smoke test (create → share → answer → results) to run against staging after every deploy and gate promotion, so that the core journey can never silently break.

### Self-hosting

73. As a self-hoster, I want `docker compose up` to give me the full core loop (app + Postgres + local email catcher), so that adoption takes minutes.
74. As a self-hoster, I want AI features to work against ollama/llamafile via configuration or degrade gracefully when absent, so that no Google dependency is required.
75. As a self-hoster, I want magic-link auth over my own SMTP and optional Google OIDC, so that login works on my infrastructure.
76. As a self-hoster, I want the workspace export format documented as a stable contract, so that migrating into my instance is a solved problem (import tool: first post-MVP ticket).

## Implementation Decisions

All load-bearing decisions are recorded as ADRs; the spec inherits them:

- **Versioning (ADR-0001):** Draft → publish → immutable Survey Version; Responses pin to the version served, never copied ("response migration" in stakeholder language = this read-time aggregation, not data movement); results aggregate by Question Identity. Immutability enforced at store layer and DB triggers.
- **Ownership (ADR-0002):** Workspaces own surveys; personal workspace auto-created; MVP hard-codes sole-member access; the workspace is the future billing and DPA boundary.
- **Anonymity (ADR-0003, amended by 0009):** anonymous Responses carry no email/IP/UA — no such columns exist on the response path; IPs only in a separate short-retention abuse log with no join path to responses; `is_anonymous` trigger-guarded immutable.
- **Voice (ADR-0004):** transcript-only; local browser recognition only when verifiably local, else MediaRecorder → WebSocket → Vertex AI Gemini pinned to europe-west4; audio in memory only, never persisted or logged; respondent reviews the Transcript; the respondent's chosen language drives recognition locale and transcription hints. Per-survey opt-in audio retention was proposed (collaborator Q4) and deliberately kept out — it stays a backlog decision requiring its own ADR and DPA language.
- **Email (ADR-0005):** Brevo behind a two-method `Sender` interface; SMTP for self-hosters; SPF/DKIM/DMARC on a dedicated subdomain; webhook-fed suppression; drip caps. CRM integration is out of scope; the interface is the future integration point.
- **Anti-abuse (ADR-0006):** ALTCHA in-app, first-party widget; zero third-party scripts on respondent pages (CI-enforced); honeypot; token-bucket rate limits; session-bound LLM tokens; per-workspace daily AI quotas plus a global daily € breaker.
- **Platform (ADR-0007):** Cloud Run, separate stg/pro projects, opentofu; one Go binary with `serve | purge | migrate` subcommands; purge cron = same binary as a scheduled Cloud Run job; WebSocket clients auto-reconnect.
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
- **Fakes only where the world ends:** the AI `Provider` (scripted streaming fake covering Generate, Transcribe, Translate, Analyze — including language-hint passthrough), the email `Sender` (capturing fake), and an injectable clock (close dates, magic-link expiry, purge windows, insight cache watermarks).
- **The purge subcommand is tested at the same level:** run against a seeded database, assert what survives, using the clock to time-travel.
- **Behavior contracts with dedicated tests:** version immutability (including frozen Localizations); anonymity (no identifying data retrievable for anonymous responses by any query the app can run); **aggregate unlinkability** (no app query associates a survey_stats counter with a response) and n<5 suppression; cross-workspace authorization denial; pin-don't-copy aggregation (the 50-v1/30-v2 scenario) now also spanning insight runs; mandatory-review gate on localization publish; original-preservation on answer translation; CSV injection safety; suppression honoring; quota and breaker trips; ALTCHA rejection of unsolved challenges.
- **Out-of-band checks:** axe-core scan of respondent pages; CI assertion that respondent pages reference only first-party origins; responsive rendering checked at phone/tablet/desktop widths; local speech feature-detection gets a JS unit test plus a documented manual browser matrix; a real end-to-end smoke test (create → share → answer → results) runs against staging after every deploy and gates promotion.
- **Prior art:** none — greenfield. This spec's seam choice *is* the prior art; the first milestone establishes the harness every later ticket reuses.

## Out of Scope

Branching/conditional logic and the drag-and-drop flow editor (first backlog item — schema-shaping, deliberately not rushed); ranking and rating-matrix question types; results sharing outside the workspace; brand preferences / creator profile; embedding surveys in external sites or email (requires a CSP/anti-bot redesign — framing is currently blocked by design); per-survey opt-in audio retention (consciously deferred; reopening it requires a new ADR, consent UX, and DPA language); survey improvement suggestions; AI images (Nano Banana 2) and sections; CRM integrations; workspace member-invite UI (schema supports it; MVP is sole-member); response editing after submit; workspace import tool (export format is the contract); Helm chart; billing/payments; the homepage repo's copy fix ("your voice is never stored") — required before launch but lives in TryEarful/homepage; GDPR legal documents (DPA template, privacy policy text, RoPA, external counsel) — the plan's gap list; EU AI Act companion additions — next pass; non-local browser speech recognition (deliberately never used, ADR-0004).

## Further Notes

- Budget: €100/month pre-revenue operating target (alerts €50/€80/€100), €200 hard cap; typical projection ≈ €85–190 with the expanded AI scope, so AI quotas start tight (plan, Appendix C).
- Execution order: M0–M8, then M10 (insights) and M11 (localization/translation), then M9 (launch last). Milestone numbers are stable ticket-import IDs, not a sequence.
- Anonymous multi-submission is an accepted trade-off, stated to creators in the UI, not a bug to fix later.
- Verify at build time: current Vertex model IDs and written no-training/EU-residency terms (owner + deadline + plan B tracked in the plan's gap list); ALTCHA Go library state; Brevo webhook formats; GeoIP database licensing.
- Vocabulary is normative per CONTEXT.md (Survey, Draft, Draft Revision, Survey Version, Question Identity, Response, Answer, Workspace, Participant, Respondent, Creator, Transcript, Close Date, Survey Status, Localization, Insight Summary, Audit Log). "Recording" deliberately does not appear except to say recordings do not exist.
- Milestone/ticket decomposition (M0–M11, 56 tickets) lives in PLAN.md; this spec is the what-and-why those tickets implement.
