# Build plan: a survey's own style (issue #16)

Design: [ADR-0018](../adr/0018-survey-style-frozen-with-the-version.md),
accepted. The questions this plan once ended with are answered there.

A creator gives a survey one of four themes, a header (a banner, a logo,
a name, a tagline and links), a footer (text and links) and a picture
for the thanks page. The style is set on the survey's Style tab, seen in
the preview, and frozen into the version at publish.

Each step below ships on its own, leaves every existing page and test
passing, and is reviewed with `make gallery` against the style guide
before it is committed (CONTRIBUTING.md, "Building a page"). Steps 1 to
5 each add their user stories to `SPEC.md` with the tests that cover
them. Nothing is tagged for production until step 5 is in: a logo is
what makes a survey that imitates somebody convincing, and the report
link and the runbook procedure have to be there before one can reach a
respondent.

Migration numbers are the next free ones at the time each step is built.

## Step 0: theme becomes display mode (size S)

**Goal.** "Theme" is free to mean the creator's choice. No behaviour
changes.

- `web/templates`: `data-theme` on `<html>` becomes `data-mode`;
  `theme(ctx)`, `themeSwitcher()` and `themeColor()` become `mode`,
  `modeSwitcher` and `modeColor`; the `js-theme-color` hook becomes
  `js-mode-color`.
- `web/static/css/app.css`: the `[data-theme]` selectors and the
  comments that call dark a theme. `web/static/js/theme.js` becomes
  `mode.js`.
- `internal/http`: the handler, its route and the cookie. The cookie is
  written as `mode`; a `theme` cookie is still read, so a saved choice
  is kept. The trust page lists the cookie under its new name.
- `docs/style-guide.md` and the wording in `web/text`: "display mode"
  wherever light and dark are meant.

**Tests.** The existing switcher tests, renamed, plus one: a request
carrying only the old `theme` cookie is drawn in that mode. `make check`
and `make e2e-smoke` pass with no other change.

## Step 1: themes (size M)

**Goal.** A creator opens the Style tab, picks Slate, Ocean or Forest,
sees it in the preview, publishes, and respondents see it in their own
display mode.

**Style guide first.**

- `docs/style-guide.md` gains "Themes": what a theme reassigns (the
  grounds, text, links, focus and ring, the accent, the filled button,
  the borders, the ground of a notice), what it may not (Signal, the
  status colours and their tints, type, space, shape), and the contrast
  table for every pair in all eight combinations of theme and display
  mode.
- `web/static/css/app.css`: `.theme-slate`, `.theme-ocean` and
  `.theme-forest` reassign the semantic tokens, each with a light block
  and a dark one written the way the dark mode is today (under the media
  query and under `data-mode="dark"`). No component rule changes.
- A contrast test (`web/static/contrast_test.go`, beside the test that
  compares the two dark blocks) reads the tokens, works out the ratio of
  every listed pair in every combination, and fails below the pair's
  threshold: 4.5:1 for text, 3:1 for the focus ring, borders that carry
  meaning and the status colours' marks. The table in the style guide is
  written from the same numbers.

**Draft and version.**

- `internal/domain`: `Draft` gains `Style Style`
  (`json:"style,omitzero"`) with `Theme string` (empty means Earful).
  Validation refuses a theme that is not one of the four. A draft whose
  style differs from the latest version's is a changed draft, so the
  editor offers Publish.
- Migration: `survey_versions` gains `style jsonb`, NULL meaning no
  style. The trigger already on the table guards it. Down fails while
  any version carries a style (ADR-0001), as the thank you migration
  does.
- `db/queries/surveys.sql` and `internal/store`: publish writes the
  column in the publish transaction; the queries that load a version for
  a respondent return it.

**Pages.**

- `web/templates`: a Style tab in `surveyTabs`, at
  `GET /surveys/{id}/style`, with one form posting to
  `POST /surveys/{id}/style` and one filled button. The theme section is
  four radio cards, each a small sample drawn in that theme's tokens.
  A link to the preview sits at the head of the page.
- `RespondLayout` takes the style and puts the theme's class on `<html>`.
  `RespondData` carries it from the served version, the preview from
  the draft, `RespondThanks` from the version the response was pinned
  to. The already answered page and the page of a closed survey that has
  a version take it from the latest version.
- A theme sheet at a development only route: every themed component on
  one page (text, muted text, links, the filled and outlined buttons,
  fields, chosen and unchosen options, progress, an error, a notice, the
  focus ring, the record button idle and recording, both footers).
- Wording in `web/text/active.en.toml` and `active.es.toml`: the tab,
  the section heading, each theme's name and one line about it, the
  notice after saving.

**Export and purge.** `internal/export`: the next format version; each
version gains `"style": {"theme": "ocean"}`, omitted when none.
`docs/export-format.md` documents it and adds a row to its Changes
table. Purge needs nothing: the column goes with its row.

**Tests at the edge** (docs/testing.md).

- Choosing a theme shows it in the preview and not on `/s/{id}`; after
  publishing, `/s/{id}` carries it and so do the thanks, already
  answered and closed pages; a response pinned to version 1 is thanked
  in version 1's theme after version 2 changes it; an unknown theme is
  refused with a message and the draft is unchanged; a survey in another
  workspace cannot be styled.
- The immutability test: `style` on a published version cannot be
  updated.
- The export carries the new format version, the style round trips, and
  a version without one has no `style` key.
- The contrast test above. The existing first party test stands.
- `e2e/tests/smoke.spec.ts`: a respondent's page in each theme is axe
  clean in both display modes.

**Gallery.** The theme sheet in all four themes, both display modes,
390px and 1280px, in English. The questions page in all four themes,
both modes, both widths, both languages. The paged question, thanks,
already answered and closed pages in Earful and one other theme. The
Style tab, as saved and with an error.

**Review.** Reviewer agents take the style guide's checklist and, for
themed pages, four more criteria: Signal is the only coral and stands
out, the focus ring is visible on every surface, a status colour still
reads as status, and the filled button is the page's one main action.

## Step 2: header and footer text (size M)

**Goal.** A creator adds a name, a tagline, links and a footer text, and
translates them.

- `internal/domain`: `Style` gains `Header{Name, Tagline, Links}` and
  `Footer{Text, Links}`; a link is a label and an address, built and
  checked by the rules `NewThankYou` applies to its link (an absolute
  `http` or `https` address, a label required). Caps: name 80
  characters, tagline and footer text 280, label 60, three links each.
  `Localization` gains the tagline, the footer text and the link labels;
  the name and the addresses are shared. The frozen `style` column holds
  the reviewed translations, as `thanks_localizations` does.
- The Style tab gains the Header and Footer sections; the translation
  pages gain the new fields, and drafting a translation with AI covers
  them as it covers the thank you message.
- `respond.templ`: a `styleHeader` above the `<h1>` (name, tagline,
  links), full on the questions page and on the first page of a paged
  survey, compact (name only) on later pages and on the thanks, already
  answered and closed pages. A `styleFooter` above Earful's footer.
  Links carry `target="_blank"` and `rel="noopener noreferrer"`.
  Earful's footer reads "Powered by Earful".

**Tests at the edge.** Each field shows in the preview, then on every
respondent page after publish; a `javascript:` address, a link with no
label, a fourth link and an overlong field are each refused with a
message; a Spanish respondent reads the Spanish tagline and labels, and
the source text where none was written; markup typed into a field is
shown as text; the heading order of the page holds with and without a
header.

**Gallery.** The questions page with the longest name, tagline and three
long links at 390px and 1280px, in both languages; with a name only;
the compact header on a paged question and on the thanks page; the Style
tab with every field filled and with each error.

## Step 3: the logo and the banner (size L)

**Goal.** A creator uploads a logo and a banner; the survey has one
mark.

**Storage.**

- Migration: `survey_images(id uuid PRIMARY KEY, survey_id uuid NOT NULL
  REFERENCES surveys, sha256 bytea NOT NULL, content_type text NOT NULL
  CHECK (content_type IN ('image/png', 'image/jpeg')), width int NOT
  NULL, height int NOT NULL, size_bytes int NOT NULL, bytes bytea NOT
  NULL, created_at timestamptz NOT NULL, UNIQUE (survey_id, sha256))`.
- A new package `internal/styleimage` (domain logic, no HTTP): identify
  the file by its content, refuse anything but PNG, JPEG and WebP, read
  the dimensions with `image.DecodeConfig` and refuse a pixel count that
  would not fit in memory, decode, scale down to the slot's size with
  `golang.org/x/image/draw`, and encode again. A banner is stored as a
  JPEG at most 1600 pixels wide; a logo as a PNG (or a JPEG if it came
  as one) at most 512 pixels a side. It is written so that pictures in
  questions (ADR-0021) can share it.
- `internal/store`: saving refuses an eleventh image for a survey;
  `GetPublishedImage(sha)` returns an image only if a published version
  of a survey that is not deleted refers to it;
  `GetDraftImage(workspace, survey, sha)` serves the preview.

**Routes.** The Style form becomes multipart, with the CSRF field first.
The upload caps are 2 MB for the banner and 1 MB for the logo, read with
a limit and refused with a message beyond it. `GET /style-image/{sha256}`
is public; `GET /surveys/{id}/style-image/{sha256}` serves the draft's
images in the creator's session. Both send the exact content type and
`Cache-Control: public, max-age=31536000, immutable` (the creator's
route `private`), in place of the `no-store` from `SecurityHeaders`.
Neither writes a counter.

**Pages.** The Header section gains two file fields, each with the
current image, a "Remove this image" checkbox, and for the logo its
alternative text, which is required and part of a Localization. Under
the logo field: "Only use a logo you have the right to use". The
respondent's header draws the banner at three to one with
`object-fit: cover` and empty alternative text, and the logo on a plate
overlapping its lower edge, both with `width` and `height` so the page
does not jump. The compact header and the creator's footer show the logo
small. Earful's footer leaves out the owl when the style has a logo.

**Export and purge.** The archive gains `images/<sha256>.<ext>`, once
per image however many versions use it; a version's `style` names the
file and its alternative text. Images count toward the archive's cap.
Purge removes `survey_images` with the survey, and removes an image no
version and not the current draft refers to once it is seven days old.

**Tests at the edge.**

- Upload a PNG from `testdata/`: the preview shows it, `/s/{id}` does
  not until publish, then does. The served bytes of a JPEG fixture with
  a GPS tag decode and carry no EXIF. A 4000 pixel photograph is
  accepted and served at 1600 pixels wide. A WebP is accepted and served
  as PNG or JPEG. An SVG, a GIF, a file over its cap, and a file named
  `.png` that holds text are each refused with a message. A logo without
  alternative text is refused.
- `GET /style-image/{sha}` is 404 for a hash only a draft refers to, for
  a deleted survey's image, and, on the creator's route, for another
  workspace's draft image.
- Fetching an image does not change the survey's opened count.
- A survey with a logo has no owl in its footer and keeps "Powered by
  Earful"; one without a logo keeps the owl.
- The respondent page's external origin test passes with images. Purge
  and export as above, the orphan rule with the fake clock.

**Gallery.** The questions page with banner and logo in all four themes,
both display modes, both widths; with a logo and no banner, a banner and
no logo; a wide logo and a tall one; a dark logo in the dark display
mode, to show the plate; the compact header with a logo; the Style tab
with both images.

## Step 4: the thanks picture (size M)

**Goal.** A creator chooses what the thanks page shows: the owl, one of
three illustrations, their own image, or nothing.

- `Style` gains `Thanks{Picture, ImageSHA, Alt}`, where the picture is
  `owl` (the default), `check`, `envelope`, `confetti`, `image` or
  `none`. An uploaded picture uses step 3's pipeline with its own slot:
  1 MB, at most 800 pixels a side, alternative text required and
  localized.
- `web/templates/brand.templ`: the three illustrations, inline SVG
  filled through classes from the theme's tokens, as the owl is. The
  style guide lists them, with where each may appear (the thanks page
  only).
- The Style tab gains the Thanks picture section: six radios with a
  small drawing of each, and a file field. The editor's thank you panel
  gains one line pointing to it.
- `RespondThanks` and the preview's submitted page draw the choice where
  the owl was; an uploaded picture sits on a plate.

**Tests at the edge.** Each choice shows on the thanks page after
publish and in the preview before; `image` with no upload is refused; a
response pinned to an earlier version is thanked with that version's
picture; `none` leaves the heading and the message.

**Gallery.** The thanks page with each of the six choices, in one theme,
both display modes; the three illustrations on the theme sheet, so they
are seen in every theme.

## Step 5: safeguards and terms (size S)

**Goal.** A respondent can report a survey, and support knows what to do
with the report.

- Earful's footer on respondent pages gains "Report this survey": a
  `mailto:` to the instance's `CONTACT_EMAIL`, with the survey's address
  in the subject. Not shown on an instance without one.
- A test that the disclosure naming the workspace is on the questions
  page of a styled survey, above the questions, in Earful's words.
- `docs/runbook.md` gains "A survey impersonates someone": checking the
  complaint, closing the survey, suspending the workspace, answering the
  reporter.
- `web/pages/terms.en.md` and `terms.es.md`: the terms, including that a
  creator may only use names and marks they are entitled to. They stay
  marked as a draft until the instance's operator has read them.

**Tests at the edge.** The report link is on the questions, thanks and
closed pages when a contact address is set, carries the survey's
address, and is absent without one.

**Gallery.** A respondent's footer with and without the report link.

## Not in these steps

- A colour chosen by hex value, and themes beyond the four.
- A workspace's own default style that new surveys start from: Survey
  Defaults (issue #19), which would copy a style into a new draft and
  needs no change to how a survey is drawn.
- Hiding "Powered by Earful", Help or the privacy page from a
  respondent's page.
- Fonts chosen by the creator.
- A live preview of the banner's crop before saving.
- A switch that turns the style off for an instance.
