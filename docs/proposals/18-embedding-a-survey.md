# Embedding a survey in another page (issue #18)

The story: a creator wants HTML they can paste into a web page or an
email so that people can answer a survey from there. The decision is
[ADR-0017](../adr/0017-embedding-a-survey-in-another-page.md); this is
the plan to build it. Every slice ships on its own, leaves `make check`
and `make e2e-smoke` green, and follows CONTRIBUTING.md: no JavaScript
required, wording in `web/text` in English and Spanish, `js-` hooks,
tests at the application edge ([docs/testing.md](../testing.md)), and
every new state in `e2e/gallery/gallery.spec.ts`.

Slice 1 answers the issue for email completely and for web pages well
enough to learn whether a frame is wanted. Slices 2 to 4 are the frame.
Slice 5 lists what is deliberately left for later.

## Slice 1 (S): a link and button snippet in the Sharing card

No header, route or migration changes.

- **What a creator sees.** In the Sharing card of an open anonymous
  survey (`lifecyclePanel` in `web/templates/surveys.templ`, under the
  share link), a heading "Put it on a page or in an email", a read only
  `<textarea>` holding the snippet, and an outlined "Copy code" button.
  The textarea is selectable without JavaScript. The snippet is not
  shown for an invited survey (its public link refuses answers), a
  draft, or a closed survey.
- **The snippet.** One `<a>` to `origin + ShareURL()` with inline styles:
  Ink background, Paper text, the pill radius, 44px height, Manrope
  falling back to the system sans serif, since an email client will not
  load Earful's font. Its label is a message (`share.snippet.label`,
  "Answer the survey") in the creator's interface language. Built by a
  function on `SurveyView` in `web/templates/views.go` and escaped by
  templ, so a survey title can never break out of the attribute.
- **Copying.** `web/static/js/share.js` finds one `.js-copy-link` today.
  It becomes every `.js-copy` button, each copying the text of the
  element its `data-copy-from` names, with the existing hidden until a
  clipboard exists behaviour. The share link's button moves to the same
  hook.
- **Style guide.** A read only code block is a new component:
  `.snippet` (the existing `--mono` stack on the Paper ground, the
  field radius, wrapping rather than scrolling sideways), added to `docs/style-guide.md`
  and `app.css` in the same change.
- **Words.** `editor.sharing.snippet.heading`, `.hint` ("Paste this
  where you want a button that opens the survey. It works in emails
  too."), `.copy`, `.copied`, and `share.snippet.label`, in
  `active.en.toml` and `active.es.toml`.
- **Tests** (`internal/http/share_test.go`):
  - an open anonymous survey's editor shows a snippet whose `href` is
    the share address and whose label is the message;
  - a survey titled with `"><script>` produces a snippet with the title
    escaped, and the snippet contains no `<script>`;
  - invited, draft and closed surveys show no snippet;
  - the snippet parses as HTML with exactly one element, an `a`.
- **Gallery.** `editor-sharing-snippet` (open anonymous survey) and the
  existing invited and closed editor states, at both widths, both
  themes, both languages.
- **Docs.** "Publish and share" in `web/pages/help.en.md` and
  `help.es.md` gains a sentence on the snippet; `SPEC.md` story
  added (see the report).

## Slice 2 (M): the list of sites a survey may be framed by

Stored and edited, with no effect on any response yet.

- **Migration** `db/migrations/NNNNN_survey_embed_origins.sql`:
  `survey_embed_origins (survey_id uuid REFERENCES surveys(id), origin
  text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (survey_id, origin))`, with a CHECK that the origin
  matches `^https?://[a-z0-9.-]+(:[0-9]{1,5})?$` as a second line of
  defence behind the parser. A comment explains that the value is
  written into a response header.
- **Purge.** `internal/purge/purge.go` gains
  `embed_origins_of_doomed_surveys` before `doomed_surveys`; the purge
  deletes children explicitly.
- **Store.** `store.Surveys.EmbedOrigins(ctx, surveyID)` and
  `SetEmbedOrigins(ctx, workspaceID, surveyID, []string)`, replacing the
  set in one transaction and scoped to the workspace.
- **Parsing.** `domain.ParseEmbedOrigin(raw string, allowHTTP bool)`:
  `url.Parse`, scheme `https` (or `http` when `allowHTTP`, set only in
  development), a host, no user info, path empty or `/`, no query or
  fragment; returns `scheme://host[:port]`, lower cased, default port
  dropped. At most 10 per survey.
- **Interface.** The settings card (`settingsPanel`) gains a textarea,
  "Sites that may show this survey in a frame", one origin a line,
  saved by the existing `POST /surveys/{surveyID}/settings`. Refusals
  are worded per line ("example.org/page is not a site address. Use
  https://example.org."). Not shown for invited surveys.
- **Export.** `workspace.json` adds `"embed_origins": []` per survey,
  `format_version` 3, and a version note in `docs/export-format.md`
  (if the owner agrees: open question 9).
- **Tests:**
  - settings save and show back a normalised list
    (`HTTPS://Example.org:443/` becomes `https://example.org`);
  - refusals for `*`, `https://*.example.org`, `https://a.example/path`,
    `javascript:alert(1)`, `https://a.example; script-src *`,
    `http://a.example` on a staging shaped instance, and an eleventh
    site;
  - another workspace cannot set a survey's sites (404);
  - the export carries the sites; the purge removes them
    (`apptest.NewIsolatedDB`, as the purge tests do).
- **Gallery.** `editor-settings-embed-empty`, `-filled`, `-error`.

## Slice 3 (M): the frame route

- **Routes** in `internal/http/routes.go`:
  `GET /e/{surveyID}` and `POST /e/{surveyID}`, beside `/s/`. The
  challenge and voice endpoints stay on `/s/` and need no copy: both
  are same origin calls from the framed page.
- **Headers.** `security.go` builds the policy from a base without
  `frame-ancestors` plus one ancestors value. `SecurityHeaders` keeps
  writing `'none'`; a helper `allowFraming(w, origins)` lets the embed
  handlers rewrite the directive after loading the survey, and writes
  nothing when the list is empty. `X-Frame-Options: DENY` stays. The
  comment that lists embedding as out of scope points at ADR-0017.
- **Handlers.** `respondPage` and `respondSubmit` take the base path
  from the route rather than assuming `/s/`. `RespondData` gains
  `Embedded bool`; `respondAction` and the language picker build
  `/e/...` when it is set. Framed pages do not call `setAnsweredCookie`
  and do not read the answered cookie. Refusals (closed, invited,
  missing, rate limited) render the same pages as `/s/`.
- **Template.** `RespondLayout` takes `Embedded`; when set, the footer
  links carry `target="_blank" rel="noopener"`, since Help and the
  privacy page cannot be drawn inside the frame.
- **Tests** (`internal/http/embed_test.go`):
  - `/s/{id}` of a survey that lists sites still sends
    `frame-ancestors 'none'` and `X-Frame-Options: DENY`
    (`TestSecurityHeaders` keeps passing unchanged);
  - `/e/{id}` with no sites sends `'none'`;
  - `/e/{id}` with two sites sends exactly those two in
    `frame-ancestors`, every other directive unchanged, `DENY` still
    present;
  - `/e/{id}` of an invited, closed, draft and deleted survey renders
    the refusal and allows no framing unless sites are listed;
  - a `POST /e/{id}` with `Sec-Fetch-Site: same-origin` stores one
    response pinned to the served version and sets no cookie; with
    `Sec-Fetch-Site: cross-site` it is refused before any handler;
  - the honeypot, form token and rate limits behave as on `/s/`
    (table driven over both paths);
  - the framed page's footer links open in a new tab and its form
    posts to `/e/`.
- **e2e** (`e2e/embed.spec.ts`, Chromium as the rest of the suite):
  a host page served by `page.route` from a second origin that is on
  the survey's list frames `/e/`, answers and submits through the
  frame, and sees the thanks page in it; a host origin not on the list
  gets a blocked frame. axe runs on the framed page.
- **Gallery.** `respond-embedded-first`, `-last`, `-thanks`, and
  `-closed`, each pictured inside a host page at 390px and 1280px.

## Slice 4 (S): the frame snippet in the Sharing card

- Shown below the link snippet when the survey lists at least one site;
  otherwise a line pointing at the setting.
- The snippet: `<iframe src="{origin}/e/{id}" title="{survey title}"
  width="100%" height="640" loading="lazy" referrerpolicy="no-referrer"
  style="border:0">` with `allow="microphone"` when voice is enabled on
  the instance, followed by a plain link to the share address.
- A second `.js-copy` button. Words in both files.
- The stats page shows a note on a survey that lists sites: opens
  include visits that scrolled to the frame.
- `docs/security-review.md` gains a framing section;
  the trust page (`web/pages/trust.*.md`) notes that a page showing a survey in a frame can
  know its visitors saw it.
- **Tests:** snippet present only with sites; `title` escaped; `allow`
  present only with voice; stats note present only with sites.
- **Gallery.** `editor-sharing-embed`, `stats-embedded-note`.

## Slice 5: later, not planned

- A partitioned (`CHIPS`) answered cookie on `/e/`, if creators miss
  the "you already answered" note in frames.
- Height that follows the content, by `postMessage` and an optional
  listener the creator pastes. Off by default.
- A choice link per option in an email (NPS, yes or no) that opens the
  survey with that answer filled in, never stored by the `GET`.

## Open questions for the owner

1. Is slice 1 enough to close the issue, with the frame left until a
   creator asks for it? It covers email in full, which a frame never
   can.
2. Allowlist only, as proposed, or also a per survey "any site" choice
   with a warning?
3. Subdomain wildcards (`https://*.example.org`) in the list, which CSP
   supports? Proposed: no.
4. The limit of 10 sites per survey: right number?
5. Opens from frames: count them as on `/s/` with a note (proposed),
   leave them out, or record them apart, which would add a metric and
   amend ADR-0009 and ADR-0012?
6. Dictation in frames: offer it (proposed, with `allow="microphone"`
   and the usual consent), or switch it off in frames for a first
   release?
7. The button label: creator's interface language (proposed), the
   survey's languages, or a text field the creator fills in?
8. An instance setting for self hosters to switch framing off
   altogether, and its default?
9. Are a survey's sites part of the export (format version 3,
   proposed), or instance configuration left out like `origin`?
10. Should a change to a survey's sites appear in its Audit Log? The
    log is derived from drafts and versions today, and settings such as
    the Close Date are not in it.
11. Should the Starter Survey's Sharing card show the snippet? It is an
    ordinary open anonymous survey, so as proposed it does.
