---
status: ready-for-agent
source: grilling session 2026-07-12/15 — see PLAN.md, CONTEXT.md, docs/adr/0001–0008
---

# Earful MVP — Specification

## Problem Statement

Teams that need honest feedback don't trust closed survey SaaS with their respondents' data, and respondents don't finish surveys that make them type. Existing tools (Typeform et al.) are proprietary, US-hosted, and text-first: creators can't self-host or leave with their data, privacy-sensitive organizations can't adopt them, and open-text answers — the valuable ones — go unanswered because typing is friction. There is no open-source, EU-hosted, voice-first survey tool that people can pay for as a service *and* walk away from with their data.

## Solution

Earful: an open-source (AGPL-3.0) survey platform, hosted in the EU (europe-west4), where respondents can *speak* their answers — transcribed instantly, with the audio never stored — and creators build surveys with AI assistance. Creators run anonymous or invited surveys, edit them safely through append-only versioning that never destroys or misattributes a response, see results aggregated across versions, and can export their entire workspace at any time to move to a self-hosted instance. Trust is the product: no third-party scripts on respondent pages, strong anonymity guarantees, transparent processor list, immutable backups.

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
8. As a survey creator, I want to edit a Draft with five question types (long text, short text, single choice, multiple choice, rating scale), so that I can build most real surveys.
9. As a survey creator, I want every save of my Draft recorded as a Draft Revision, so that no editing work is ever lost.
10. As a survey editor, I want an Audit Log of who changed what and when, so that collaboration is accountable.
11. As a survey creator, I want to publish my Draft as an immutable Survey Version, so that what respondents saw can never be silently altered.
12. As a survey creator, I want to keep editing after publishing and publish again, so that improving a live survey is safe and ordinary.
13. As a survey creator, I want reworded questions to keep their Question Identity across versions, so that results stay comparable over time.
14. As a survey creator, I want an optional Close Date and a manual close/reopen control, so that responses stop arriving when the fieldwork ends.
15. As a survey creator, I want a dashboard listing my Workspace's surveys with their state (draft, open, closed), so that I can find and manage them.

### AI-assisted creation

16. As a survey creator, I want to describe my goal in a prompt and watch AI-generated draft questions stream in, so that I start from a solid draft instead of a blank page.
17. As a survey creator, I want generated questions to arrive as editable Draft content restricted to the five supported types, so that AI output is never a special case.
18. As a workspace owner, I want AI usage counted against a daily workspace quota, so that one enthusiastic teammate cannot burn the budget.

### Answering — the respondent experience

19. As a respondent, I want to open a share link and answer one question at a time with a progress indicator, so that the survey feels effortless.
20. As a respondent, I want to answer with only a keyboard, with a screen reader, or with JavaScript disabled, so that accessibility is never the price of polish.
21. As a respondent, I want the survey page to load no third-party scripts, so that answering doesn't expose me to trackers.
22. As a respondent, I want a closed survey to tell me clearly it no longer accepts responses, so that I'm not left guessing.
23. As a respondent mid-fill when a new version is published, I want my submission to count against the version I was actually shown, so that my answers are never misattributed.

### Answering — voice

24. As a respondent, I want to answer text questions by speaking, so that I can give rich answers without typing.
25. As a respondent, I want my speech recognized locally in the browser when that is verifiably possible, so that my voice doesn't leave my device unnecessarily.
26. As a respondent whose browser can't do local recognition, I want my audio streamed to Earful's EU transcription and immediately discarded, so that speaking stays possible without my voice being kept.
27. As a respondent, I want to see and edit the Transcript before it becomes my Answer, so that the record says what I meant.
28. As a respondent, I want a clear consent moment before first microphone use stating that my voice is never stored, so that I can decide informed.
29. As a respondent, I want typing always available as an alternative, so that voice is a convenience, never a requirement.
30. As a respondent who exceeds the voice quota, I want a graceful fallback to typing with a clear message, so that I can still finish.

### Anonymous surveys

31. As an anonymous respondent, I want no email, IP address, or user-agent stored with my Response, so that "anonymous" means what it says.
32. As an anonymous respondent, I want to pass a lightweight in-page challenge (ALTCHA) rather than a third-party CAPTCHA, so that proving I'm human doesn't identify me.
33. As a survey creator, I want accidental double-submits softly deduplicated, so that my results aren't polluted by twitchy fingers (accepting that anyone with the link can answer repeatedly — the stated trade-off of anonymity).

### Invited surveys

34. As a survey creator, I want to import participant emails by paste/CSV with duplicates removed, so that building the audience is quick.
35. As a Participant, I want a unique personal link by email, so that answering requires no account.
36. As a survey creator, I want invites drip-sent under a per-workspace hourly cap, so that deliverability and reputation are protected.
37. As a Participant, I want exactly one submission tied to my link, with an "already submitted" page afterwards, so that results are one-per-person.
38. As a survey creator, I want bounced or complaining addresses automatically suppressed from future sending, so that we never spam.
39. As a survey creator, I want each Response linked to the Participant's email in an Invited Survey, so that I know who answered what.

### Results and export

40. As a survey creator, I want results aggregated across all Survey Versions by Question Identity, with wording changes labelled per version, so that editing never hides or distorts history.
41. As a survey creator, I want distributions for choice and rating questions and a transcript list for text questions, so that I can read the story at a glance.
42. As a survey creator, I want per-survey CSV export that opens cleanly and safely in spreadsheet tools, so that analysis can continue elsewhere.
43. As a workspace owner, I want a one-click full Workspace export (documented JSON + CSVs, async, expiring download), so that I can leave for a self-hosted instance at any time — the trust promise made real.

### Data lifecycle and trust

44. As a survey creator, I want deleting a survey or response to be a soft-delete restorable by support for 30 days, so that mistakes aren't catastrophic.
45. As an operator, I want a purge job that hard-deletes soft-deleted data older than 30 days, expires stale tokens, and trims the abuse log — runnable by hand locally and on a schedule in production, so that retention promises are kept mechanically.
46. As a data subject, I want an erasure fast-path that support can trigger immediately (skipping the 30-day wait), so that GDPR requests complete within 24 hours.
47. As a respondent, I want the survey landing page to disclose who the controller is, whether the survey is anonymous, and how voice is processed, so that I understand before answering.
48. As a privacy-conscious visitor, I want a public trust page listing processors, the no-recordings promise, EU hosting, and the leave-anytime export, so that I can verify the claims.

### Operations (Earful as a service)

49. As an operator, I want staging and production in separate GCP projects with scale-to-zero staging, so that isolation is real and idle costs are ~zero.
50. As an operator, I want budget alerts at 50/80/100% of €200/month across both projects, so that cost surprises are impossible.
51. As an operator, I want a global daily AI budget breaker that disables AI endpoints and alerts me when tripped, so that abuse can't bankrupt the product.
52. As an operator, I want per-IP and per-survey rate limits, honeypot fields, and noindex on respondent pages, so that bots and LLM scrapers get nothing cheap.
53. As an operator, I want daily Cloud SQL exports into a retention-locked immutable bucket with a self-managing 30-day rolling window, so that ransomware with full app-project access still cannot destroy backups.
54. As an operator, I want PITR backups, uptime checks, error/latency alerts, and a drilled runbook (deploy, rollback, restore, erasure, breaker trip, breach basics), so that one person can run this calmly.

### Self-hosting

55. As a self-hoster, I want `docker compose up` to give me the full core loop (app + Postgres + local email catcher), so that adoption takes minutes.
56. As a self-hoster, I want AI features to work against ollama/llamafile via configuration or degrade gracefully when absent, so that no Google dependency is required.
57. As a self-hoster, I want magic-link auth over my own SMTP and optional Google OIDC, so that login works on my infrastructure.
58. As a self-hoster, I want the workspace export format documented as a stable contract, so that migrating into my instance is a solved problem (import tool: first post-MVP ticket).

## Implementation Decisions

All load-bearing decisions are recorded as ADRs; the spec inherits them:

- **Versioning (ADR-0001):** Draft → publish → immutable Survey Version; Responses pin to the version served, never copied; results aggregate by Question Identity at read time. Immutability enforced at both store layer and DB triggers (UPDATE/DELETE rejected except for the purge role).
- **Ownership (ADR-0002):** Workspaces own surveys; personal workspace auto-created; MVP hard-codes sole-member access; the workspace is the future billing and DPA boundary.
- **Anonymity (ADR-0003):** anonymous Responses carry no email/IP/UA — there are no such columns on the response path; IPs exist only in a separate short-retention abuse log with no join path to responses; `is_anonymous` is trigger-guarded immutable.
- **Voice (ADR-0004):** transcript-only; local browser recognition only when verifiably local, else MediaRecorder → WebSocket → Vertex AI Gemini pinned to europe-west4; audio held in memory only, never persisted or logged; respondent reviews the Transcript before it becomes an Answer.
- **Email (ADR-0005):** Brevo behind a two-method `Sender` interface (send + webhook ingestion); SMTP implementation for self-hosters; SPF/DKIM/DMARC on a dedicated subdomain; webhook-fed suppression list; drip caps.
- **Anti-abuse (ADR-0006):** ALTCHA challenges generated/verified in-app, widget served first-party; zero third-party scripts on respondent pages (CI-enforced); honeypot; token-bucket rate limits; session-bound LLM tokens; per-workspace daily AI quotas plus a global daily € breaker.
- **Platform (ADR-0007):** Cloud Run, separate stg/pro projects, opentofu; one Go binary with `serve | purge | migrate` subcommands; the purge cron is the same binary scheduled as a Cloud Run job; WebSocket clients auto-reconnect (60-min platform cap).
- **Backups (ADR-0008):** daily Cloud SQL Admin API export to a retention-locked bucket in a separate backups project; lifecycle rule owns the rolling 30-day window; export credentials are create-only.
- **Stack:** Go stdlib HTTP + a-h/templ + hand-written CSS/JS with progressive enhancement (every form works without JS); pgx + sqlc + goose; coder/websocket; server-side sessions in Postgres with CSRF protection; strict self-only CSP, HSTS, Referrer-Policy no-referrer; slog JSON with a scrubbing rule — no emails, tokens, transcripts, or answer content in logs.
- **AI:** one `Provider` interface (`Generate`, `Transcribe`, both streaming); Vertex implementation for stg/pro (model IDs — gemini-3.5-flash/pro — are configuration), OpenAI-compatible implementation for local dev; all usage metered into quota accounting.
- **Schema shape** (condensed from the plan's data model; encodes the pin-don't-copy and anonymity decisions):

  ```
  workspaces ─< surveys(is_anonymous IMMUTABLE, close_at, deleted_at)
                 ├─ survey_drafts(1:1) ─< draft_revisions(append-only)
                 ├─< survey_versions(immutable) ─< questions(question_identity_id)
                 └─< participants(email, token_hash, suppression state)
  responses(version_id, participant_id NULLABLE — NULL forever when anonymous)
     └─< answers(question_id, question_identity_id, value)
  ai_usage(workspace/day metering) · abuse_log(no FK to responses) · suppressions
  ```

- **Deliberate non-decisions:** GDPR paperwork (DPA, privacy policy, RoPA) is tracked as the gap list in the plan, not built as product features in this spec except where noted (disclosure page, trust page, erasure fast-path).

## Testing Decisions

- **One seam: the application edge.** Tests boot the real server in-process (real routing, middleware, templ rendering, WebSockets) against a real Postgres, and drive it exactly as a browser would — HTTP requests, form posts, WS dials. Assertions are on external behavior only: status codes, rendered HTML, streamed frames, rows a user could observe via the UI or export. No test reaches into internal packages.
- **Fakes only where the world ends:** the AI `Provider` (scripted streaming fake), the email `Sender` (capturing fake — invite and magic-link tests assert on captured mail), and an injectable clock (close dates, magic-link expiry, the 30-day purge window).
- **The purge subcommand is tested at the same level:** run against a seeded database, assert what survives, using the clock to time-travel.
- **Behavior contracts that must have dedicated tests:** version immutability (UPDATEs rejected), anonymity (no identifying data retrievable for anonymous responses by any query the app can run), cross-workspace authorization denial, pin-don't-copy aggregation (the 50-v1/30-v2 scenario from ADR-0001), CSV injection safety, suppression honoring, quota and breaker trips, ALTCHA rejection of unsolved challenges.
- **Out-of-band checks:** axe-core accessibility scan of respondent pages and a CI assertion that respondent pages reference only first-party origins; local speech feature-detection gets a small JS unit test plus a documented manual browser matrix (Chrome/Safari/Firefox, desktop/mobile).
- **Prior art:** none — greenfield. This spec's seam choice *is* the prior art; the first milestone establishes the harness (compose Postgres or testcontainers) that every later ticket reuses.

## Out of Scope

Answer translation; cross-response themes and per-reply AI summaries; survey improvement suggestions; AI images (Nano Banana 2) and sections; workspace member-invite UI (schema supports it; MVP is sole-member); response editing after submit; workspace import tool (export format is the contract; import is the first post-MVP ticket); Helm chart; billing/payments; the homepage repo's copy fix ("your voice is never stored") — required before launch but lives in TryEarful/homepage; GDPR legal documents (DPA template, privacy policy text, RoPA) — tracked as the plan's gap list; non-local browser speech recognition (deliberately never used, ADR-0004).

## Further Notes

- Budget ceiling €200/month across both environments; typical projection ≈ €75–170 (plan, Appendix C); the AI breaker and budget alerts are launch-blocking, not nice-to-haves.
- Anonymous multi-submission is an accepted trade-off, stated to creators in the UI, not a bug to fix later.
- Verify at build time: current Vertex model IDs and written no-training/EU-residency terms; ALTCHA Go library state; Brevo webhook formats.
- Vocabulary in this spec is normative per CONTEXT.md (Survey, Draft, Draft Revision, Survey Version, Question Identity, Response, Answer, Workspace, Participant, Respondent, Transcript, Close Date, Audit Log). "Recording" deliberately does not appear except to say recordings do not exist.
- Milestone/ticket decomposition (M0–M9, 48 tickets) lives in PLAN.md; this spec is the what-and-why those tickets implement.
