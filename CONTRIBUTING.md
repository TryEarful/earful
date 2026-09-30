# Contributing

## Getting set up

```sh
docker compose --profile app up --build   # full stack: app, Postgres, mail catcher
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
- **Every page follows the [style guide](docs/style-guide.md).** A new
  page or feature uses its tokens, components, owl moods and wording
  rules rather than new ones, and is reviewed against the guide's
  checklist before it is merged (see "Building a page" below). A change
  that needs something the guide does not have adds it to the guide in
  the same pull request.
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

## Markup, styling and behaviour

How pages look and speak is in [docs/style-guide.md](docs/style-guide.md),
and ADR-0016 records why. `make gallery` shows every page for review.

### Building a page

1. **Start from what exists.** Use the tokens at the top of
   `web/static/css/app.css` and the components the style guide lists: a
   page is a stack of `.card`s, with one filled button for its main
   action and outlined ones for the rest. A new colour, size or
   component is a change to the guide first.
2. **Write the words in `web/text`,** in English and Spanish, in
   sentence case, short, with no dashes. The tests reject wording in a
   template and a dash in any message.
3. **Hook behaviour onto `js-` classes** (below), never onto styling
   classes.
4. **Add the page to the gallery** (`e2e/gallery/gallery.spec.ts`) in
   every state a person can see it in: empty, full, an error.
5. **Review it.** Run `make gallery` and review every picture of the
   page against the checklist in the style guide ("Reviewing a page").
   For anything more than a small change, have reviewer agents do this:
   give each one a group of pages, the pictures and axe report from the
   gallery, the style guide and its checklist, and ask for a pass or
   fail per criterion with an exact fix for each failure. The agents
   read and report; fixes are made in one place, since every page shares
   one stylesheet. Repeat until each page passes twice in a row.
6. **Run `make check` and `make e2e-smoke`,** and read their exit codes,
   not a filtered part of their output.

A class does one job. A class that starts with `js-` is how a script or
a test finds an element (`js-voice-button`, `js-survey-card`), and it is
never styled. Any other class is for styling, and scripts and tests never
select by it. A state a script sets and the stylesheet shows is an
`is-` class (`is-recording`) or a `data-` attribute. With that split, the
look of a page can change without breaking what it does, and a test
fails only when behaviour does. `web/static/static_test.go` fails the
build if a stylesheet styles a `js-` class.

## What must not be written down

Everything in this repository ships to everyone who runs Earful, and
`docs/runbook.md` in particular is read as *their* operating manual, not
as a log of ours. Two rules follow.

**Never commit anything instance-specific.** Not because any single item
is a secret, but because collectively they describe one deployment to
anyone reading, and none of it means anything to the next operator:

| Never | Instead |
|---|---|
| Project ids, bucket names, account or organisation ids | The `<sfx>` placeholder; `earful-pro-<sfx>` |
| Revision names, job execution ids, image digests | Say which kind of thing, not which one |
| An operator's alert address or contact | `<your alert address>` |
| Spend figures, row counts, retention expiry dates | The threshold or window that produced them |
| Anything from `*.tfvars`, or any credential in any form | Nothing. These are gitignored for a reason |

**A procedure records what it taught, not what one run produced.** "The
clone inherits deletion protection, so disable it first" saves the next
person twenty minutes and is true on every instance. "Clone
`earful-drill` restored at 10:04, 18 tables" is true on exactly one, and
on that one it is already in the provider's own logs. Keep the first
kind, drop the second. If you need the raw evidence, keep it outside the
repository — `docs/runbook.local.md` is gitignored for this.

This applies to generated text as much as to written text: an agent
pasting a command's real output into a document is the most likely way
any of the above lands in a commit.

## Commits

One logical change per commit, with a subject in the imperative under 72
characters and a body explaining why the change is the right one. Commit
directly against `main` for small work; anything that changes behaviour
should say so in `PLAN.md` and `SPEC.md` in the same commit, because
those two files are expected to describe the software as it actually is.
