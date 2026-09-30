# Working on Earful

Read [CONTRIBUTING.md](CONTRIBUTING.md) first. It sets the rules for
code, comments, tests and commits, and they apply to agents as they do to
people.

## Any change to a page

Earful's pages follow one design, described in
[docs/style-guide.md](docs/style-guide.md) (why: ADR-0016). A new page or
feature is built from that guide, not beside it:

- Use the tokens and components in `web/static/css/app.css`. Do not add a
  colour, size, font or component that is not in the guide; if one is
  needed, add it to the guide in the same change.
- One filled button per page. Signal coral only where a microphone is
  open. The owl only in the moods and places the guide lists.
- Wording goes in `web/text/active.en.toml` and `active.es.toml`, in
  sentence case, short, with no dashes.
- Scripts and tests find elements by `js-` classes only.
- Add the page, in each state it can be in, to
  `e2e/gallery/gallery.spec.ts`.

## Checking the design with agents

Before committing a change to what a page looks like or says:

1. Run `make gallery` (it writes to `GALLERY_DIR`) to picture every page
   at 390px and 1280px, light and dark, English and Spanish, with an axe
   report.
2. Start reviewer agents, one per group of pages the change touches
   (for example: sign in and account; dashboard and editor; results and
   stats; the respondent's pages; Help and the documents). Give each the
   gallery folder, its pages, and the checklist in the style guide's
   "Reviewing a page" section. Ask for a pass or fail per page and
   criterion, and for each failure the exact fix: the CSS rule, the
   template change, or the message to reword. Reviewers only read and
   report.
3. Apply the fixes yourself, in one place, then run `make check` and
   `make e2e-smoke` and read their exit codes.
4. Picture the pages again and send them back to the same reviewers.
   Stop when every page passes every criterion in two rounds in a row.

A reviewer's advice is checked before it is applied: a suggestion that
would break a test, drop information a test relies on, or contradict the
guide is declined, and the reason is given to the reviewer in the next
round.
