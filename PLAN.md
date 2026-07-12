# Earful — MVP Plan

Open-source, AI-enhanced, voice-first surveys. Go + templ + plain HTML/CSS/JS. Trust, privacy and self-hostability are the differentiators. This plan is the processable output of the grilling session of 2026-07-12; the vocabulary lives in `CONTEXT.md`, the load-bearing decisions in `docs/adr/0001–0007`.

- Repo: https://github.com/TryEarful/earful (AGPL-3.0, already in place)
- SaaS: app.tryearful.com (pro), stg.tryearful.com (stg) — Cloud Run, separate GCP projects, europe-west4
- Homepage: https://tryearful.com (TryEarful/homepage, GitHub Pages)
- Budget ceiling: €200/month across both environments, alert-guarded

## Decision index (ADRs)

| ADR | Decision |
|---|---|
| 0001 | Responses pin to immutable Survey Versions; no copy-forward; results aggregate by Question Identity |
| 0002 | Workspaces own surveys from day 1; personal workspace auto-created |
| 0003 | Anonymity is strong (no email/IP/UA near responses) and immutable at creation |
| 0004 | Voice is transcript-only; audio never stored; local-first, else Vertex Gemini @ europe-west4 |
| 0005 | Email via EU ESP (Brevo) behind a two-method interface |
| 0006 | No third-party scripts on respondent pages; ALTCHA in-app |
| 0007 | Cloud Run over GKE for the SaaS MVP; K8s later is a redeploy, not a rewrite |

## MVP scope

**In:** create → share → answer → results loop; Draft/publish versioning with audit log; anonymous and invited surveys; lean five question types (long text with voice, short text, single choice, multiple choice, rating scale); one-question-at-a-time respondent UX; close dates; voice answering (transcript-only); AI question generation (streamed); workspace export (JSON + CSV); Google OIDC + magic-link auth; ALTCHA + rate limits; soft-delete + 30-day purge cron; docker compose self-hosting.

**Out (post-MVP backlog, ordered):** answer translation; cross-response themes & per-reply summaries; survey improvement suggestions; AI images (Nano Banana 2) for header/section/question; workspace member invites UI; response editing (answer revisions); sections; workspace import tool; Helm chart; billing.

## Architecture overview

One Go binary, three subcommands: `earful serve` (HTTP + WebSocket server), `earful purge` (the 30-day cleanup, run manually in dev, Cloud Scheduler → Cloud Run job in stg/pro), `earful migrate` (goose migrations, also run on deploy). Server-rendered HTML via templ; plain CSS and vanilla JS with progressive enhancement — every form works without JS; JS adds one-question-at-a-time flow, voice, and streaming. WebSockets stream LLM output token-by-token (client auto-reconnects; Cloud Run caps connections at 60 min). Postgres via pgx + sqlc; no ORM. AI calls go through an internal `ai` package with two implementations: Vertex AI (Gemini Flash/Pro, pinned europe-west4) for stg/pro, and an OpenAI-compatible/ollama/llamafile client for local dev. Model IDs (`gemini-3.5-flash`, `gemini-3.5-pro`) are config, not code — verify current IDs at build time.

```
Browser ── HTTPS ──> Cloud Run (earful serve) ──> Cloud SQL Postgres
   │                     │        │
   │ WS (stream)         │        └──> Brevo (SMTP/API): magic links, invites
   │                     └──> Vertex AI Gemini @ europe-west4 (transcribe, generate)
   └── mic ──> local SpeechRecognition only if verifiably local; else audio streams
               through serve → Vertex; audio discarded, transcript returned
```

### Repo layout

```
cmd/earful/            main; subcommands serve|purge|migrate
internal/domain/       entities + invariants (no SQL, no HTTP)
internal/store/        pgx + sqlc-generated queries
internal/http/         handlers, middleware (sessions, CSRF, rate limit, security headers)
internal/ws/           websocket streaming (coder/websocket)
internal/ai/           Provider interface; vertex + openai-compatible impls; quotas + budget breaker
internal/email/        Sender interface (Send, HandleWebhook); brevo + smtp impls
internal/antibot/      ALTCHA challenges, honeypot, abuse log
internal/auth/         Google OIDC, magic links, sessions
web/templates/         .templ files
web/static/            css/, js/ (vanilla; altcha widget vendored)
db/migrations/         goose
db/queries/            sqlc
deploy/compose.yaml    local dev (app + postgres + ollama optional)
deploy/opentofu/       modules/ + envs/stg + envs/pro; state in GCS
docs/adr/              ADRs; CONTEXT.md at root
```

### Data model sketch (Appendix A has columns)

`users`, `workspaces`, `workspace_members` — workspace-owned everything (ADR-0002). `surveys` (settings: anonymity flag immutable, close date, soft-delete) → `survey_versions` (immutable, publish-numbered) → `questions` (per version, carries stable `question_identity_id`, position, type). `survey_drafts` (one per survey) + `draft_revisions` (append-only autosave → audit log). `responses` (pinned to version; participant_id nullable and NULL forever for anonymous) → `answers` (typed value per question). `participants` (invited surveys: email, unique token, invite/bounce state). `magic_link_tokens`, `sessions`, `abuse_log` (short retention, no join path to responses), `ai_usage` (per-workspace/day quota accounting), `suppressions` (email bounces/complaints).

## Milestones and tickets

Tracer-bullet ordering: each milestone ends with something demonstrable. Tickets: **Goal / Acceptance criteria / Deps**. IDs are stable for issue-tracker import.

### M0 — Foundations (repo walks)

- **M0-T1 Skeleton & toolchain.** Goal: `cmd/earful` with serve|purge|migrate stubs, templ + sqlc + goose wired, Makefile. AC: `make dev` serves a templ-rendered page; `make check` runs vet, staticcheck, govulncheck, templ generate --check. Deps: —
- **M0-T2 docker compose local env.** Goal: compose with app + Postgres (+ optional ollama profile). AC: fresh clone → `docker compose up` → app on :8080 with migrations applied. Deps: M0-T1
- **M0-T3 CI.** Goal: GitHub Actions: checks + image build. AC: PR runs checks; main builds/pushes image to Artifact Registry. Deps: M0-T1
- **M0-T4 Config & secrets convention.** Goal: env-based config (12-factor), documented in README; no secrets in repo. AC: all later tickets consume config through one package. Deps: M0-T1
- **M0-T5 Structured logging & scrubbing.** Goal: slog JSON to stdout; middleware never logs emails, tokens, transcripts, or answer content. AC: log-scrub unit tests pass. Deps: M0-T1

### M1 — Walking skeleton on staging

- **M1-T1 opentofu baseline.** Goal: modules + envs/stg: project, Cloud Run service, Cloud SQL (smallest), Artifact Registry, Secret Manager, GCS state bucket. AC: `tofu apply` in envs/stg from zero produces a serving URL. Deps: M0-T3
- **M1-T2 Domain & TLS.** Goal: stg.tryearful.com mapped to Cloud Run. AC: HTTPS with managed cert. Deps: M1-T1
- **M1-T3 Deploy pipeline.** Goal: main → deploy stg automatically; tags → pro (pro project arrives in M9). AC: merge to main visible on stg in <10 min; migrations run safely before traffic. Deps: M1-T1
- **M1-T4 Health & uptime.** Goal: /healthz (DB ping) + Cloud Monitoring uptime check + alert channel (email). AC: killing DB fires an alert. Deps: M1-T1
- **M1-T5 Budget guardrails.** Goal: GCP budget on stg project: 50/80/100% of its share of €200. AC: alerts configured and test-fired. Deps: M1-T1

### M2 — Auth & workspaces

- **M2-T1 Sessions.** Goal: server-side sessions in Postgres; Secure/HttpOnly/SameSite=Lax cookies; CSRF tokens on all mutations. AC: session fixation + CSRF tests pass. Deps: M0
- **M2-T2 Google OIDC login.** Goal: sign-in with Google (OIDC only, no extra scopes). AC: login → user row + session. Deps: M2-T1
- **M2-T3 Magic-link login.** Goal: email → single-use token (15-min expiry, hashed at rest) → session; rate-limited per email+IP. AC: replay and expiry tests pass. Deps: M2-T1, M4-email-sender (see M4-T6; for stg use console sender stub until then)
- **M2-T4 Personal workspace auto-creation.** Goal: first login creates workspace + sole membership; all queries workspace-scoped. AC: authorization tests prove cross-workspace access impossible. Deps: M2-T2
- **M2-T5 Account deletion (user-level).** Goal: user can delete account → soft-delete + purge pipeline. AC: deletion marks rows; purge removes after window. Deps: M2-T4, M8-T2

### M3 — Survey building

- **M3-T1 Survey + Draft CRUD.** Goal: create survey (name, anonymity flag immutable at creation, optional close date), edit draft with lean-five question types, positions. AC: draft accepts no responses; anonymity unchangeable via any path. Deps: M2-T4
- **M3-T2 Draft revisions (autosave).** Goal: every save appends a Draft Revision. AC: revision list shows who/what/when; storage append-only. Deps: M3-T1
- **M3-T3 Publish → immutable Version.** Goal: publish freezes draft into survey_versions + questions with stable Question Identity (reword keeps identity; new question = new identity; delete ends it). AC: published rows immune to UPDATE (guarded by triggers or store-layer invariant + tests); share link serves latest version. Deps: M3-T1
- **M3-T4 Audit log view.** Goal: derived who-changed-what from revisions + publishes. AC: editors see the trail. Deps: M3-T2, M3-T3
- **M3-T5 Survey list & lifecycle.** Goal: workspace dashboard; manual close/reopen; close-date enforcement. AC: closed surveys refuse new responses with a friendly page. Deps: M3-T3

### M4 — Answering (anonymous + invited)

- **M4-T1 Respondent renderer.** Goal: one-question-at-a-time flow, progress bar, keyboard navigation, WCAG 2.1 AA basics, no-JS fallback renders the whole form. AC: axe-core clean on respondent pages; works with JS disabled. Deps: M3-T3
- **M4-T2 Anonymous submission path.** Goal: public link → ALTCHA-gated submit; response pinned to served version; zero identifying data stored (ADR-0003). AC: schema + code review confirm no IP/UA columns on the response path; double-submit soft-deduped by cookie but allowed. Deps: M4-T1, M4-T5
- **M4-T3 Participants & unique links.** Goal: CSV/paste import of emails; per-participant long random token; one submission per token; "already submitted" page. AC: token guessing infeasible (≥128-bit); duplicate email import deduped. Deps: M3-T3
- **M4-T4 Invite sending.** Goal: drip-send invites (e.g. 200/hour/workspace cap) via email interface; suppression list honored; bounce/complaint webhooks ingested. AC: bounced address never re-mailed; caps enforced. Deps: M4-T3, M4-T6
- **M4-T5 Anti-abuse layer.** Goal: ALTCHA challenge (in-app, ADR-0006), honeypot, per-IP+per-survey token-bucket rate limits, `noindex` + robots.txt on respondent pages, abuse_log (30-day retention, unlinked). AC: load test shows limiter holds; no third-party requests on respondent pages (CI check counts origins). Deps: M4-T1
- **M4-T6 Email sender (Brevo).** Goal: `email.Sender` with Brevo impl + SMTP impl (self-host); SPF/DKIM/DMARC on mail.tryearful.com; webhook endpoint. AC: DMARC passes; webhook updates suppressions. Deps: M1-T2
- **M4-T7 Security headers & CSP.** Goal: strict CSP (self-only, no inline), HSTS, Referrer-Policy: no-referrer (protects tokened URLs), X-Content-Type-Options. AC: securityheaders.com A on stg. Deps: M4-T1

### M5 — Voice (transcript-only, ADR-0004)

- **M5-T1 Local recognition detection.** Goal: feature-detect verifiably-local SpeechRecognition (`processLocally`-style); use only then; otherwise fall to server path. AC: non-local browser recognition is never invoked; decision table documented per browser. Deps: M4-T1
- **M5-T2 Server transcription fallback.** Goal: MediaRecorder → WS stream → `ai.Provider.Transcribe` (Vertex Gemini Flash @ europe-west4) → streamed transcript; audio held in memory only, never persisted, never logged. AC: code review + grep gates (no writes of audio buffers); soak test shows bounded memory. Deps: M4-T5, M6-T1
- **M5-T3 Transcript review UX.** Goal: respondent sees/edits transcript before answer is accepted; mic is optional; typing always available; mic-permission consent copy states "voice is never stored". AC: usability pass on mobile Safari + Chrome + Firefox (Firefox = server path or typing). Deps: M5-T1, M5-T2
- **M5-T4 Voice quotas.** Goal: per-response-session transcription budget (n seconds), per-survey/day and per-IP caps wired into ai_usage. AC: exceeding quota degrades to typing with clear message. Deps: M5-T2, M6-T2

### M6 — AI question generation

- **M6-T1 ai.Provider interface.** Goal: `Generate(stream)`, `Transcribe(stream)` behind one interface; Vertex impl (europe-west4, no-training config verified) + openai-compatible local impl (ollama/llamafile). AC: swap via env var; integration test against ollama in CI. Deps: M0-T4
- **M6-T2 Quotas + budget breaker.** Goal: ai_usage accounting; per-workspace daily caps; global daily € breaker that hard-disables AI endpoints and alerts. AC: breaker trips in test; alert fires. Deps: M6-T1
- **M6-T3 Generate-survey flow.** Goal: creator prompt → WS-streamed draft questions (lean five types only) → editable draft; requires auth; counts against quota. AC: p95 first-token < 2s on stg; output validates against question schema. Deps: M6-T1, M6-T2, M3-T1

### M7 — Results & export

- **M7-T1 Results view.** Goal: per-survey results aggregated across versions by Question Identity, wording labelled per version (ADR-0001); counts/distributions for choice+rating; transcript list for text. AC: cross-version scenario from the ADR renders correctly (50 v1 + 30 v2). Deps: M4-T2
- **M7-T2 CSV export per survey.** Goal: responses as CSV (one row per response, columns per question identity, version column). AC: opens clean in Excel/Sheets; injection-safe (`=+-@` prefixed). Deps: M7-T1
- **M7-T3 Workspace export.** Goal: one button → async job → downloadable archive: documented JSON (surveys, versions, questions, responses, participants) + CSVs. Format versioned and documented in repo (`docs/export-format.md`) as the future import contract. AC: export of a seeded workspace round-trips against the format doc; download link expires. Deps: M7-T2

### M8 — Data lifecycle & trust

- **M8-T1 Soft delete.** Goal: surveys/responses/workspaces delete → `deleted_at`; excluded everywhere; restorable by support until purge. AC: soft-deleted invisible in app + exports. Deps: M3-T5
- **M8-T2 Purge cron.** Goal: `earful purge` hard-deletes soft-deleted >30 days, expires magic links, trims abuse_log + draft-revision retention; idempotent; dry-run flag. Deployed as Cloud Scheduler → Cloud Run job (same binary). AC: manual local run + scheduled stg run both verified; purge logged (counts only). Deps: M8-T1
- **M8-T3 Erasure fast-path.** Goal: admin/support action "purge now" for GDPR erasure requests (skip the 30-day wait); participant lookup by email. AC: erasure completes < 24h from request; documented in runbook. Deps: M8-T2
- **M8-T4 Trust page content.** Goal: /trust on homepage: processor list, no-recordings promise, EU hosting, export/leave-anytime, AGPL. AC: copy reviewed against Appendix B; homepage "keep every recording" line replaced with "your voice is never stored". Deps: Appendix B
- **M8-T5 Respondent-facing disclosure.** Goal: survey landing shows controller (workspace name), anonymity status, voice processing note, link to privacy notice. AC: shown for both survey kinds; consent moment before first mic use. Deps: M4-T1

### M9 — Production launch

- **M9-T1 Pro environment.** Goal: envs/pro opentofu (separate project), app.tryearful.com, deploy-on-tag, Cloud SQL with automated backups + PITR (7 days) + deletion protection. AC: restore drill from backup succeeds on a scratch instance. Deps: M1, M8
- **M9-T2 Full alert set.** Goal: budgets (both projects, 50/80/100% of €200 combined), uptime, error-rate, p95 latency, Cloud SQL disk/CPU, ai_usage anomaly, breaker-trip alert. AC: alert test-fire checklist complete. Deps: M9-T1
- **M9-T3 Security pass.** Goal: dependency scanning (govulncheck in CI + Renovate), secrets scan, authz test suite re-run, rate-limit soak, backup-restore drill logged. AC: findings triaged to zero criticals. Deps: M9-T1
- **M9-T4 Runbook.** Goal: docs/runbook.md — deploy, rollback, restore, erasure request, breaker trip, ESP suppression check, incident basics (72h breach duty). AC: a second person can execute each procedure. Deps: M9-T2
- **M9-T5 Launch checklist.** Goal: DNS cutover, homepage copy fix live, trust page live, seed feedback survey (dogfood), announce. AC: real survey completes end-to-end on pro via voice on a phone. Deps: all

## Appendix A — Data model (key columns)

```
users(id, email uniq, google_sub nullable, created_at, deleted_at)
workspaces(id, name, created_at, deleted_at)
workspace_members(workspace_id, user_id, role='owner', created_at)
surveys(id, workspace_id, title, is_anonymous bool IMMUTABLE, close_at nullable,
        created_by, created_at, deleted_at)
survey_drafts(id, survey_id uniq, structure jsonb, updated_by, updated_at)
draft_revisions(id, draft_id, structure jsonb, saved_by, saved_at)  -- append-only
survey_versions(id, survey_id, number, published_by, published_at)  -- immutable
questions(id, version_id, question_identity_id, type, text, options jsonb,
          required bool, position)                                   -- immutable
question_identities(id, survey_id, created_at)
participants(id, survey_id, email, token_hash uniq, invited_at, bounced_at,
             submitted_at, deleted_at)
responses(id, survey_id, version_id, participant_id nullable /*NULL=anonymous*/,
          submitted_at, deleted_at)   -- no IP, no UA, ever (ADR-0003)
answers(id, response_id, question_id, question_identity_id, value jsonb)
sessions(id, user_id, expires_at, created_at)
magic_link_tokens(token_hash, email, expires_at, used_at)
suppressions(email, reason, created_at)
ai_usage(id, workspace_id nullable, survey_id nullable, kind, tokens, est_cost,
         day)                        -- quota + breaker accounting
abuse_log(id, ip, path, at)          -- separate retention, no FK to responses
```

Enforcement notes: immutability of `survey_versions`/`questions`/`answers` via store-layer invariants + DB triggers rejecting UPDATE/DELETE (except purge role); `is_anonymous` guarded by trigger; all workspace-scoped queries go through sqlc with workspace_id parameters.

## Appendix B — GDPR: processors and gap list

Roles: for **respondent data**, the customer (workspace) is controller and Earful is **processor**. For **account data** (users, workspaces, billing later), Earful is controller. This split drives the DPA and the privacy policy.

### Sub-processors (disclose on /trust; sign DPAs)

| # | Processor | Purpose | Data | Region | Notes |
|---|---|---|---|---|---|
| 1 | Google Cloud (Cloud Run, Cloud SQL, GCS, Secret Manager, Logging) | Hosting | All service data | europe-west4 | Google Cloud DPA + SCCs; US parent → CLOUD Act residual risk (see gaps) |
| 2 | Google Vertex AI (Gemini Flash/Pro; later Nano Banana 2) | Transcription fallback, generation; later translation/insights/images | Audio in transit (never at rest), prompts, transcripts | europe-west4 pinned | Verify in writing: no-training terms, abuse-log retention, EU residency guarantee |
| 3 | Brevo (FR) | Magic links, invites, bounces | User + participant emails | EU | DPA; suppression list holds emails |
| 4 | Google Identity (OIDC) | Login | Email, Google subject ID | — | Only when user chooses Google login |
| — | Browser speech vendors (Google/Apple) | *Avoided by design* | — | — | Non-local browser recognition is never invoked (ADR-0004); keep note in policy in case this changes |

GitHub hosts code only — no service data.

### Gap list (not yet compliant / needs work — tackle in this order)

1. **DPA for customers** (Earful-as-processor) — template + acceptance flow at workspace creation. Blocks any serious customer.
2. **Privacy policy + sub-processor page** with change-notification mechanism (email on new sub-processor).
3. **Respondent-facing transparency** — M8-T5 covers the product side; policy text still needed (who is controller, retention, rights contact).
4. **Erasure workflow** — M8-T3 fast-path; document that anonymous responses are unerasable *because they contain no personal data* (feature, not bug — state it).
5. **Backup retention vs erasure** — purged data survives in PITR/backups up to 7 more days; document the window in policy (standard practice).
6. **Records of Processing (RoPA)** — internal doc, one page per processing activity.
7. **Vertex AI terms verification** — written confirmation of no-training + EU processing + abuse-logging retention for the exact APIs used; revisit zero-retention config; CMEK later.
8. **CLOUD Act honesty** — EU region ≠ immunity from US parent jurisdiction. Mitigations: CMEK w/ external keys (later), sovereign-cloud options (expensive). Until then: state residual risk plainly on /trust; it is more credible than pretending.
9. **Breach runbook** — 72-hour notification duty (M9-T4 includes it); define who notifies whom.
10. **Retention schedule** — responses: life of survey; abuse_log ≤30d; draft revisions (pick: 90d?); Cloud Logging 30d; magic links minutes; Brevo logs per their retention.
11. **Cookies/localStorage audit** — essential-only (session, CSRF, progress) → no consent banner needed; keep it that way (ADR-0006 helps).
12. **Tokened URLs are personal data** — participant links + magic links: Referrer-Policy no-referrer (M4-T7), short expiry, revocation for participant links (post-MVP UI).
13. **Log hygiene** — M0-T5; audit before launch that no email/transcript/answer reaches Cloud Logging.
14. **Minors** — ToS: surveys must not target under-16s in MVP; revisit later.
15. **EU AI Act posture** — current features are minimal-risk; when AI images ship, label AI-generated imagery; note transparency duties for AI-assisted analysis when insights ship.
16. **DPO / EU representative** — likely not required at MVP scale; document the reasoning for not appointing one yet.

## Appendix C — Budget (target ≤ €200/mo, both envs)

| Item | Est. €/mo | Guard |
|---|---|---|
| Cloud Run stg (scale to zero) | 0–5 | — |
| Cloud Run pro (1 warm min-instance) | 10–20 | max-instances cap |
| Cloud SQL stg (db-f1-micro tier) | 8–12 | — |
| Cloud SQL pro (small + PITR + backups) | 30–55 | disk alert |
| Vertex Gemini (transcription + generation) | 20–40 typical | ai_usage caps + daily € breaker |
| Brevo | 0–25 | free tier → first paid tier |
| Egress, GCS, Logging, Scheduler, Secret Mgr | 5–15 | logging exclusion filters |
| **Typical total** | **≈ €75–170** | budgets at €100/€160/€200 |

Hard guards: GCP Budgets on both projects (50/80/100% of combined €200), Vertex quota limits, the in-app daily breaker (M6-T2), Cloud Run max-instances, log exclusion filters. Prices are estimates — verify at M1/M9 and tune alerts to reality.

## Appendix D — Self-hosting story

docker compose is the contract: app + Postgres; optional ollama/llamafile profile for AI parity (`ai.Provider` via env). Email = any SMTP. Auth = magic link (SMTP) + optional Google OIDC credentials. No Google dependency required to run core surveys; AI features degrade gracefully when no provider is configured. Workspace export (M7-T3) is the migration vehicle; the documented export format is the import contract; import tool is the first post-MVP ticket. AGPL-3.0 keeps SaaS forks honest. Helm chart: post-MVP, community-driven.

## Appendix E — Local development

`docker compose up` → app :8080, Postgres, mailpit (or console sender) for emails, ollama profile for AI. `earful purge --dry-run` runnable by hand (the same binary prod schedules). Seed command creates demo workspace/survey. `make check` = vet + staticcheck + govulncheck + templ + sqlc verify + tests. No cloud account needed for full core-loop development.

## Appendix F — Technology choices (minimal-dependency Go)

| Concern | Choice | Note |
|---|---|---|
| HTTP | stdlib net/http (1.22+ mux) | no framework |
| Templates | a-h/templ | as specified |
| DB | pgx + sqlc + goose | typed SQL, embedded migrations |
| WebSockets | coder/websocket | minimal, maintained |
| AI | Vertex AI Go SDK / genai with Vertex backend + location; openai-compatible client for local | model IDs in config |
| Anti-bot | altcha-org/altcha-lib-go + vendored widget | ADR-0006 |
| Email | Brevo API + net/smtp fallback | ADR-0005 |
| Auth | OIDC (coreos/go-oidc) + hand-rolled magic links | no auth SaaS |
| Logging | slog JSON | scrubbing middleware |
| CSS/JS | hand-written, no frameworks, progressive enhancement | accessibility budget: axe clean |

---

*Process note: tickets are sized for one-agent/one-sitting execution where possible; M0→M4 is the critical path; M5/M6 parallelize after M4; every milestone leaves stg demonstrable.*
