# Earful

Open-source, AI-enhanced, voice-first surveys. Trust, privacy and
self-hostability are the differentiators.

**Status**: M0 (Foundations), M2 (Auth & workspaces) and M3-T1…T5 (Survey
building) complete — sign in with a magic link or Google, get a personal
workspace, build surveys from eight question types, and publish them into
immutable versions. Answering them comes next (M4). See [PLAN.md](PLAN.md)
for the full milestone/ticket breakdown and current status, and
[SPEC.md](SPEC.md) for the product spec these tickets implement.

## Quickstart (docker compose)

```sh
docker compose up --build
```

Fresh clone to running app in one command: Postgres starts, migrations run
once via a one-shot `migrate` service, then the app serves on
[localhost:8080](http://localhost:8080). Add `--profile ollama` to also
start a local Ollama instance for AI features (once wired up in later
milestones).

```sh
docker compose down       # stop
docker compose down -v    # stop and wipe the Postgres volume
```

### Signing in locally

Emails never leave your machine in local development: the compose stack
delivers them to **mailpit**, a local mail catcher. Open
[localhost:8025](http://localhost:8025) — that's your inbox for magic
links and survey invites alike. Click the sign-in link, press **Sign
in**, and you land on your dashboard with a personal workspace created.
(The link only signs you in on the button press — a plain GET just shows
a confirmation page, so link-prefetching email scanners can't burn your
single-use token.)

Running without compose (`make dev`)? The default sender is `console`,
which prints emails to stdout instead — grep the sign-in link from there.

Google login is optional. To enable it locally, create an OAuth client
with redirect URI `http://localhost:8080/auth/google/callback` and set
`GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` in your environment before
`docker compose up`; without them the login page shows email sign-in only.

## Local development without Docker

```sh
make tools   # installs pinned templ/goose/staticcheck/govulncheck; checks for sqlc
make dev     # go run ./cmd/earful serve, on :8080
```

`make dev` needs a database (from M2 onward `serve` refuses to start
without one). Start just Postgres and point `DATABASE_URL` at it:

```sh
docker compose -f deploy/compose.yaml up -d postgres
export DATABASE_URL='postgres://earful:earful@localhost:5433/earful?sslmode=disable'
make migrate && make dev
```

See `.env.example` for the full set.

### Dev toolchain

| Tool | Version | Notes |
|---|---|---|
| Go | 1.25.12+ (see `go.mod`) | required by `a-h/templ`; `GOTOOLCHAIN=auto` fetches it automatically if your default Go is older |
| templ | v0.3.1020 | **must match the installed CLI exactly** — templ enforces CLI/runtime version parity |
| sqlc | 1.31.1 | install via `brew install sqlc` (macOS) or a [prebuilt binary](https://docs.sqlc.dev/en/stable/overview/install.html) — **do not** `go install` it; its cgo-heavy embedded Postgres parser can fail to build |
| goose | v3.24.1 | via `make tools` |
| staticcheck, govulncheck | latest | via `make tools`; deliberately float to latest rather than a pin — see the comment in `Makefile` |

`make tools` installs everything except sqlc into your Go bin directory
(wherever `go install` already resolves it — respects an existing `GOBIN`,
e.g. from mise/asdf). **macOS note**: freshly-linked cgo binaries (goose
uses a cgo SQLite driver) can get killed on first run with no error output
until ad-hoc re-signed; `make tools` does this automatically after every
install, so you shouldn't need to think about it.

### Makefile targets

| Target | What it does |
|---|---|
| `make tools` | Install pinned dev tools |
| `make generate` | Regenerate templ + sqlc output (committed to the repo) |
| `make dev` | Run the server locally |
| `make build` | Build the `bin/earful` binary |
| `make check` | Full CI-equivalent check: vet, staticcheck, govulncheck, templ/sqlc drift, tests |
| `make test` | Bring up compose Postgres, run `go test ./...` |
| `make e2e-smoke` | Playwright + axe suite against the compose stack, at phone/tablet/desktop widths |
| `make migrate` | Run `earful migrate` against `DATABASE_URL` |
| `make purge` | Run `earful purge --dry-run` |
| `make generate-check` | Fail if committed templ output is stale (used by `make check`) |
| `make compose-up` / `make compose-down` | `docker compose` against `deploy/compose.yaml` |
| `make docker-build` | Build the production image locally |

## Configuration

Environment-based (12-factor), read through a single package
(`internal/config`) that every subcommand and future milestone extends —
see `.env.example` for a copy-pasteable starting point.

| Variable | Default | Notes |
|---|---|---|
| `APP_ENV` | `development` | `development`, `staging`, or `production`. Anything but `development` marks cookies `Secure` |
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | *(empty)* | Postgres DSN; required by `serve`, `migrate` and `purge` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `BASE_URL` | `http://localhost:8080` | Externally-visible origin; sign-in links in emails are built from it |
| `EMAIL_SENDER` | `console` | Only `console` for now — prints emails to stdout. Brevo/SMTP arrive with M4-T6 |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | *(empty)* | Set both to enable Google login; unset hides it |
| `GOOGLE_OIDC_ISSUER` | `https://accounts.google.com` | Override only for testing against a fake issuer |

No secrets are committed to this repo, and the binary never auto-loads
`.env` files — see the note at the top of `.env.example`.

## Testing

See [docs/testing.md](docs/testing.md) for the full test-harness
convention (the "application edge" seam, why compose-Postgres over
testcontainers, and the Playwright MCP front-end verification steps).
Short version: `make test` or `make check`.

## Docs

- [PLAN.md](PLAN.md) — milestones, tickets, status
- [SPEC.md](SPEC.md) — product spec, user stories, testing decisions
- [CONTEXT.md](CONTEXT.md) — vocabulary
- [docs/adr/](docs/adr/) — architecture decision records
- [docs/testing.md](docs/testing.md) — test harness conventions

## License

AGPL-3.0 — see [LICENSE](LICENSE).
