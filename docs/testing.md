# Testing

This documents the test harness convention established in M0 and reused,
unchanged, by every later milestone. See SPEC.md's "Testing Decisions" for
the rationale; this file is the how-to.

## One seam: the application edge

Tests boot the real server in-process (real routing, real middleware, real
templ rendering) against a real Postgres, and drive it exactly as a browser
would: plain HTTP requests, assertions on status codes and rendered HTML.
No test reaches into internal packages.

`internal/http.NewHandler(cfg, logger, deps)` builds that real handler.
`internal/apptest` wraps it for tests:

```go
app := apptest.New(t, apptest.Options{})   // real handler, real Postgres,
                                           // fake clock + captured outbox
client := app.Login(t, apptest.UniqueEmail("alice"))  // full magic-link flow
resp, _ := client.Get(app.Server.URL + "/dashboard")
```

`apptest.App` exposes the three things a test needs to steer the world:

| Field / method | Use |
|---|---|
| `app.Server` | the running `httptest.Server` |
| `app.Emails` | captured outbox (`All`, `To`, `Last`) — read links like an inbox |
| `app.Clock` | injectable clock: `Advance` past token expiry, close dates, purge windows |
| `app.Login(t, addr)` | drives the real sign-in flow, returns a cookie-jar client |
| `app.CSRFToken(t, client)` | reads the session's CSRF token off a rendered form |
| `apptest.Options{Env: "staging"}` | production-shaped instance (asserts `Secure` cookies) |
| `apptest.Options{GoogleIssuer: ...}` | enables Google login against `internal/oidctest` |
| `apptest.Options{AI: fake}` | injects an `ai.Fake`; without it an instance has no AI at all, which is itself the "degrades gracefully when absent" proof |
| `apptest.Options{AIQuota: 1}` | a quota small enough to trip, for the refusal paths |
| `app.LoginAsSuperAdmin(t, addr)` | a session on the support surfaces (invite codes, erasure, metrics) |
| `apptest.NewIsolatedDB(t, "purge")` | a separate database for tests that operate on the whole of it — see below |

**Fakes stop at the world's edge.** `internal/oidctest` is a real OIDC
issuer (discovery, JWKS, RS256-signed ID tokens) so everything on our side
of the boundary — go-oidc discovery, code exchange, full token
verification — runs for real; only Google itself is replaced.

**Assert on what a reader sees.** templ escapes HTML entities, so
`sam's workspace` renders as `sam&#39;s workspace`. Test assertions go
through the `bodyContains` helper, which unescapes first.

## Why docker-compose Postgres, not testcontainers-go

Tests get their database from `TEST_DATABASE_URL` (falling back to
`DATABASE_URL`), pointed at the same Postgres service `deploy/compose.yaml`
already defines for local dev. We deliberately did not add
testcontainers-go or any other Go test-dependency for this:

1. Minimal-dependency Go (PLAN.md Appendix F) — zero extra Go modules, no
   Docker SDK client, no ryuk sidecar.
2. `docker compose` is already the self-hosting contract (PLAN.md
   Appendix D) — reusing the same Postgres service for dev and tests means
   there's exactly one way to run Postgres locally, not two.
3. CI needs nothing extra: `make check`/`make test` run the identical
   `docker compose -f deploy/compose.yaml up -d --wait postgres` a
   developer runs locally — no separate provisioning path to keep in sync.

`internal/apptest.NewDB` **skips** (not fails) the calling test when no
database is reachable, so plain `go test ./...` stays usable without
Docker for fast unit-test iteration. `make test`, `make check`, and CI
always provision Postgres first, so the integration tests genuinely run
there.

## Test isolation: unique data per test (decided at M2-T1)

Tests share one database and **each test creates its own users and
workspaces** with unique email addresses (`apptest.UniqueEmail`). Nothing
is truncated between tests and no test runs inside a rolled-back
transaction.

Why this rather than transaction-per-test or truncate-between-tests:

1. **The seam forbids it.** Tests drive the app over HTTP, so the handler
   owns its own connections from the pool — a test cannot hand it a
   transaction to run inside without reaching past the application edge.
2. **Workspace scoping is already the isolation boundary.** Every
   product query is workspace-scoped (ADR-0002). Tests using distinct
   workspaces are isolated by the same mechanism that isolates real
   customers — and a test that fails because another test's data leaked
   into it is reporting a real authorization bug, which is exactly the
   signal we want.
3. **Tests stay parallel.** `t.Parallel()` everywhere; truncation would
   force serialization.

Consequence to respect when writing tests: **never assert on global
counts** ("there are 3 users"), only on data the test itself created.
Aggregate-shaped assertions (M7 stats, M10 insights) must be scoped to a
survey or workspace the test owns.

### The purge exception (M8-T2)

`earful purge` is global by definition, which breaks the shared-database
model twice over: a global DELETE cannot run against data other tests are
still using, and two purges running concurrently see each other's
half-finished work. Both showed up immediately — foreign-key violations
mid-transaction and a dry run reporting 63 surveys where the real run
removed 27.

So the purge suite gets its own database (`apptest.NewIsolatedDB`, which
creates and migrates `<base>_purge` on first use) and **does not call
`t.Parallel()`**. Within that it behaves normally: seed a workspace
through the real application, time-travel `app.Clock`, assert on what
survives.

This is the only test in the repository that needs its own database. If a
second one appears, ask hard whether the feature really has to be
global.

### Private-beta helpers (M12)

`apptest.Options{BetaMode: true}` boots an instance with the invite-code
gate on. Three helpers cover the flows: `MintBetaCode` (DB-direct, the
CLI's path), `SignupWithCode`, and `LoginWithPassword` — the last two
assert they land on /dashboard, mirroring `Login`. The magic-link
helpers are untouched: every pre-M12 test runs with the gate off, which
is itself the regression proof that `BETA_MODE=false` changes nothing.
The beta suite lives in [beta_test.go](../internal/http/beta_test.go);
its one non-obvious member is the concurrent same-code race, which
pins the used_at-IS-NULL consume as a database fact rather than a
code-path hope.

## Running tests

```sh
make test    # brings up compose Postgres, runs go test ./...
make check   # tools + generate + vet + staticcheck + govulncheck + templ/sqlc drift + test
```

> **Use `make test`, not bare `go test ./...`.** When no database is
> reachable, `apptest.NewDB` **skips** rather than fails — deliberate, so
> unit-test iteration works without Docker, but it means a bare `go test`
> can print `ok` for every package while every database-backed test
> quietly skipped. `make test` starts Postgres first, so `ok` means what
> you think it means. If you do run `go test` directly, check the skip
> count: `go test ./... -v | grep -c -- '--- SKIP'` should be 0.

## The e2e suite (`make e2e-smoke`)

`e2e/` holds a Playwright + axe-core suite that drives the real compose
stack in a real browser — the paged respondent flow, the invisible ALTCHA
solve, magic links read from mailpit's API. Every test runs at phone,
tablet and desktop widths; one test runs with JavaScript disabled; axe
scans gate the respondent, login and dashboard pages. It runs in CI (the
`e2e` job) and — since the cloud milestone — doubles as the **staging
promotion gate**: `.github/workflows/deploy.yml` runs the whole suite
against every staging deploy with `E2E_BASE_URL` pointed at the service.
Staging has no mailpit (Cloud Run can't receive SMTP), so there the suite
sets `E2E_LINK_SOURCE=logging` and fetches magic links from Cloud Logging
instead — staging deliberately runs the console email sender, whose
stdout lines land as log entries (`E2E_LOG_PROJECT` +
`E2E_GCP_ACCESS_TOKEN` configure the fetch; the deploy workflow supplies
both). Locally nothing changes: mailpit stays the default source.

```sh
make e2e-smoke   # compose up + npm install + playwright test
```

The suite covers the core loop, voice (with a synthesized microphone),
AI question generation with JavaScript on and off, results, the CSV
download, an Insight Summary, and a workspace export — 40 tests across
phone, tablet and desktop. The compose stack runs `AI_PROVIDER=scripted`
by default, which is what makes the AI paths testable without a model.

### The suite asks what the instance offers

One suite gates three different configurations — a laptop with the
scripted provider, the CI compose stack, and staging running Vertex — so
it probes rather than assumes. `helpers.ts` reads server-rendered
markers (`#ai-generate`, `form[data-voice-path]`, `#insights`) and the
AI tests **skip with a reason** where the capability is absent, after
asserting that the page around it still works. That is the product rule
under test, not a concession: an absent capability is an absent feature
(Appendix D), so a survey editor with no drafting panel must still add
questions by hand, and a respondent page with no mic must still take a
typed answer.

`E2E_AI_MODE` says what is behind the seam:

| Value | Where | What the voice test asserts |
|---|---|---|
| `scripted` (default) | laptop, CI compose | The exact transcript lands in the textarea and can be edited. |
| `real` | staging (set by `deploy.yml`) | Consent → capture → socket → `Transcribed`, with no error and the field still editable. |

The distinction exists because the microphone emits a **tone, not
speech**. The scripted provider ignores the audio and returns a canned
sentence; a real transcriber correctly hears no words. Asserting on the
words against a real model would fail the promotion gate for the model
being right, so against `real` the suite asserts everything around the
words instead.

### The microphone is synthesized, not faked by the browser

`fakeMicrophone` (in `helpers.ts`) replaces `getUserMedia` with a
MediaStream built from the page's own `AudioContext`. It reads like a
workaround and is worth explaining, because the obvious alternative is
broken: **Chromium's `--use-fake-device-for-media-capture` is a no-op in
Chrome 149** — with the flag set, `enumerateDevices()` returns only real
hardware. So the suite was recording from the developer's actual
microphone on a laptop (silence, which the scripted provider ignored)
and failing outright on CI runners, which have no audio device at all
(`NotFoundError: Requested device not found`).

A stream generated in the page needs no hardware, sounds the same on
every machine, and stops the test suite opening anybody's mic.
Everything downstream of `getUserMedia` — PCM conversion, the socket,
the transcript, the caps — is untouched; what is skipped is the
browser's own device plumbing, which is not ours to test.

Two more things worth knowing:

- The suite signs in **once** (a setup project saves storage state).
  Signing in per test would trip the app's own per-IP magic-link rate
  limit — that's the product working, not a test bug. The make target
  restarts the app first so repeated local runs start with fresh
  in-memory limiters.
- The axe gate is strict (`violations == []`). It has already caught real
  defects: muted-text contrast at 4.34:1 and answer controls that relied
  on the fieldset legend instead of a programmatic label.

## Guards that fail the build

Five tests exist to keep a rule true as the code grows, rather than to
check today's behaviour. They are worth knowing before you trip one:

| Guard | Where | Rule |
|---|---|---|
| Metered AI | `internal/http/ai_meter_guard_test.go` | Every `ai.Provider` call has an `aiMeter.Check` in the same function. It has caught two real gaps — a wired-up-but-unchecked call, and a translation batch checking quota once for twenty calls. |
| Aggregate unlinkability | `internal/http/stats_test.go` | No query mentions `survey_stats` together with `responses`/`answers`, and the table holds no FK to either (ADR-0009). |
| Audio non-persistence | `internal/voice/voice_test.go` | The one package holding audio has no way to write it anywhere (ADR-0004). |
| No third-party origins | `internal/http/respond_test.go` | Respondent pages reference only first-party URLs (ADR-0006). |
| Immutability | `internal/store/immutability_test.go` | Published versions, questions, revisions, localizations and insight runs refuse UPDATE/DELETE in raw SQL — the deliberate exception to the HTTP-only seam. |

## Front-end verification (Playwright MCP)

Respondent- and creator-facing pages are also verified interactively via
the Playwright MCP tools (`mcp__playwright__*`) during implementation, and
the steps are documented here so anyone can re-run them by hand:

**M0 placeholder page** (re-run after any change to `web/templates/home.templ`
or `internal/http/routes.go`):

1. `docker compose up --build -d`
2. `mcp__playwright__browser_navigate` to `http://localhost:8080/`
3. `mcp__playwright__browser_snapshot` — confirm the accessibility tree
   shows an `h1` "Earful" and the tagline paragraph
4. `mcp__playwright__browser_console_messages` (level: error) — confirm no
   unexpected errors (a `favicon.ico` 404 is expected and harmless; no
   favicon is in scope for M0)
5. `docker compose down`

**M2 sign-in and account flow** (re-run after any change to
`web/templates/auth.templ`, `web/templates/app.templ`, or the auth
handlers):

1. `docker compose up --build -d`, then wait for `docker compose ps` to
   report the app **healthy** (the container probes its own `/healthz`)
2. Navigate to `http://localhost:8080/login` — snapshot should show the
   email form, and **no** "Continue with Google" unless
   `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET` are set
3. Fill the email field, submit → "Check your email" page
4. Read the link from the app's stdout (the console sender is the local
   inbox):
   ```sh
   docker compose logs app | grep -o 'http://localhost:8080/auth/magic/verify?token=[A-Za-z0-9_-]*' | tail -1
   ```
5. Navigate to that link → **"Confirm sign-in"** page. The GET must not
   sign you in; only the button does (email-scanner protection)
6. Click **Sign in** → `/dashboard`, heading shows `<local-part>'s workspace`
7. Click **Account** → identity and 30-day deletion copy; click **Delete my
   account** → `/goodbye`
8. Navigate to `/dashboard` → redirected to `/login` (session revoked)
9. Confirm the log never leaked the credential:
   ```sh
   docker compose logs app | grep 'magic/verify'   # token must read %5BREDACTED%5D
   ```
10. `docker compose down`

**M3 survey building** (re-run after changes to `web/templates/surveys.templ`
or the survey handlers):

1. `docker compose up --build -d`; sign in via the M2 steps above
2. **Create**: `/surveys/new` → title, leave "Anonymous survey" selected →
   *Create survey*. Lands on the editor with a **Draft** chip
3. **Add questions**: add a *Long text* question, then a *Single choice*
   one with three options (one per line). Both appear in the list
4. **Publish**: *Publish version 1* → notice reads "Published version 1",
   the chip flips to **Open**, and the version list shows version 1 with
   your address and the time
5. **Reword keeps identity** (the ADR-0001 contract, visible in the DOM):
   note the identity in a question form's action URL, expand the first
   question, change its wording, *Save question*. The identity in the
   action URL is unchanged, the page still says "live version 1", and the
   publish button now offers version 2
6. **Audit log**: *View audit log* → each draft save and the publish, newest
   first, each attributed
7. **Lifecycle**: *Close survey* → chip reads **Closed**; *Reopen survey* →
   back to **Open**
8. **Phone width**: resize to 390×844 and confirm no horizontal scroll:
   ```js
   document.documentElement.scrollWidth <= window.innerWidth
   ```
   (this check caught a header overflow that affected every signed-in page)
9. Console errors: none expected beyond the `favicon.ico` 404
10. `docker compose down`

## Fakes at the true external boundaries

Live as of M2:

- **email `Sender`** — `internal/email.Capture` records messages; tests
  read sign-in links out of it exactly as a user reads their inbox.
  `email.Console` is the dev-time equivalent (prints to stdout).
- **clock** — `internal/clock.Fake`; `Advance` drives magic-link expiry
  today, close dates / purge windows / insight watermarks later.
- **OIDC provider** — `internal/oidctest`, a real issuer with fake
  identities (see above).

- **AI `Provider`** — `internal/ai.Fake`, scripted streaming outputs per
  operation, recording every request (including the transcription
  language hint M11-T3 asserts on). It can also pace fragments
  (`FragmentDelay`) and die mid-stream (`StreamErr`, `StreamErrAfter`),
  because "arrives token by token" and "survives a provider hanging up"
  are both behaviours worth pinning.
- **A development-only provider** — `AI_PROVIDER=scripted` produces
  deterministic, well-formed output for every operation with no model
  running. It is what lets the browser suite exercise voice, generation
  and insights on a laptop, and it is refused at boot outside
  `APP_ENV=development` (serving invented content to a real user would
  be a lie, so it is an invariant rather than a convention).

Three integration tests are opt-in, and each is the only witness that a
wire format matches reality:

```sh
# Vertex, against the real API with your own ADC (M6-T1). Last run
# 2026-07-25 against the staging project with gemini-2.5-flash (and
# gemini-2.5-pro for the analyze tier): generation and transcription
# both green. Those ids are the best europe-west4 offers; 3.x is
# global-only and deliberately unused (ADR-0011). Set VERTEX_TEST_AUDIO to a 16-bit PCM
# WAV to include the voice half.
VERTEX_TEST_PROJECT=earful-stg-xxxx VERTEX_TEST_MODEL=gemini-2.5-flash \
  VERTEX_TEST_AUDIO=/tmp/speech.wav \
  go test ./internal/ai/ -run Vertex_Integration -v

# whisper.cpp, against a real model (M5)
say -o /tmp/speech.wav --data-format=LEI16@16000 "the capital of France is Paris"
WHISPER_TEST_MODEL=$HOME/models/ggml-base.bin WHISPER_TEST_AUDIO=/tmp/speech.wav \
  go test ./internal/ai/ -run WhisperCLI_Integration -v
```

The OpenAI-compatible one is the same shape: point it at a real backend
with

```sh
# ollama
AI_TEST_BASE_URL=http://localhost:11434/v1 AI_TEST_MODEL=<model> go test ./internal/ai/ -run Integration -v
# llamafile (start it with --server --port 8081)
AI_TEST_BASE_URL=http://localhost:8081/v1 AI_TEST_MODEL=<gguf name> go test ./internal/ai/ -run Integration -v
```

Global-sum caution (learned twice now): tests that assert on sums spanning
the shared database (ai_usage costs today; founder metrics later) must use
per-**run**-unique data — a fixed magic date accumulates rows across
repeated runs — and assert deltas from a baseline, not absolutes.

## Test coverage log

SPEC.md's numbered user stories carry an inline `[tested](...)` link once
implemented and covered, e.g.:

```
1. As a survey creator, I want to log in with my Google account... [tested](internal/http/auth_google_test.go)
```

M0 shipped no numbered story (it is pure infrastructure); M2 covers
stories 1–5, M3 covers 6–16 (17 and 18 wait for M4's renderer).

Infrastructure and behaviour covered so far:

- Application-edge harness — `internal/apptest/apptest.go`, exercised by
  `internal/http/home_test.go`
- Config loading — `internal/config/config_test.go`
- Log scrubbing — `internal/logging/scrub_test.go`
- Magic-link login: happy path, scanner-prefetch safety, replay, expiry,
  per-email and per-IP rate limits, enumeration safety —
  `internal/http/auth_magic_test.go`
- Google OIDC login: happy path, state mismatch, subject backfill,
  unverified email, hidden-when-unconfigured —
  `internal/http/auth_google_test.go`
- Sessions and CSRF: fixation, server-side logout, cookie attributes per
  environment, CSRF matrix, cross-site rejection —
  `internal/http/session_test.go`
- Workspaces: auto-creation, stability across logins, per-user isolation,
  auth-required routes — `internal/http/workspace_test.go`
- Account deletion and health: soft-delete + session revocation,
  re-registration, `/healthz` — `internal/http/account_test.go`
- Token entropy/hashing — `internal/auth/tokens_test.go`; rate limiter —
  `internal/antibot/ratelimit_test.go`; clock — `internal/clock/clock_test.go`;
  email senders — `internal/email/email_test.go`
- Survey building end to end: creation, all eight question types,
  validation messages, publish, republish refusal, identity preservation
  across rewording, reorder/delete, close/reopen, Close Date via the fake
  clock, audit log, soft delete, and cross-workspace denial on every
  survey route — `internal/http/surveys_test.go`
- Database-enforced invariants (see the seam exception below) —
  `internal/store/immutability_test.go`
- Survey/question domain rules — `internal/domain/survey_test.go`

### The one exception to the HTTP-only seam

`internal/store/immutability_test.go` issues SQL directly. That is
deliberate and narrow: ADR-0001's immutability and ADR-0003's fixed
anonymity are enforced by database triggers precisely so they hold against
paths the application does not offer — a future admin script, a migration,
a psql session. A test restricted to our own routes could only show that
no handler performs the mutation, not that the mutation is impossible,
which is the actual guarantee. Every other test stays at the application
edge.
