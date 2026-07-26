# Contributing

## Getting set up

```sh
docker compose up --build   # full stack: app, Postgres, mail catcher
make check                  # what CI runs: vet, staticcheck, govulncheck, drift, tests
make e2e-smoke              # Playwright + axe, three viewport widths
```

[README.md](README.md) covers configuration and the toolchain;
[docs/testing.md](docs/testing.md) explains the test harness, which is
worth reading before writing a test — this project tests at the
application edge and does not reach into internal packages.

## What the review looks for

- **Every feature works without JavaScript.** Scripts in
  `web/static/js/` are enhancements; the server renders a working page
  first. A change that only works with JavaScript enabled will be asked
  to grow a server-rendered path.
- **Respondent pages stay first-party.** No third-party scripts, fonts
  or requests (ADR-0006). A build-time test enforces this.
- **Accessibility is not optional.** The axe gate treats violations as
  failures, and the browser suite runs at phone, tablet and desktop
  widths.
- **Architectural decisions live in [docs/adr/](docs/adr/).** If a change
  contradicts one, the ADR is amended in the same pull request or the
  change is rejected. Several invariants are enforced by database
  triggers and build-time tests precisely so that a rule cannot be
  quietly dropped later.

## Comments and documentation

The repository is public and read by people deciding whether to trust the
software. Comments carry weight, so they follow one rule: **explain the
constraint and why the obvious alternative is wrong — do not narrate how
the problem was discovered.**

| Avoid | Prefer |
|---|---|
| Recounting an incident, or who found it | The technical reason alone |
| "we", "our", "I" | Passive voice, or name the subject |
| A date embedded in a comment | The rule the date produced |
| Colourful phrasing about consequences | Plainly what breaks, and for whom |

A comment earns its place by saving the next reader from reintroducing a
bug. Referencing a `SPEC.md` story, a `PLAN.md` ticket or an ADR is
encouraged — that is traceability, not narrative.

Records belong in records: `PLAN.md`'s status log and the drill log in
[docs/runbook.md](docs/runbook.md) are the places where dates and
outcomes are written down, factually.

## Commits

One logical change per commit, with a subject in the imperative under 72
characters and a body explaining why the change is the right one. Commit
directly against `main` for small work; anything that changes behaviour
should say so in `PLAN.md` and `SPEC.md` in the same commit, because
those two files are expected to describe the software as it actually is.
