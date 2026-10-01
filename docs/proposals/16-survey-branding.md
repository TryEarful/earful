# Proposal: a survey's own look (issue #16)

Design: [ADR-0018](../adr/0018-survey-branding-frozen-with-the-version.md).
Status: proposed, awaiting the owner's answers to the questions at the end.

A creator gives a survey one colour from the Voice palette and an image
at its start, its end, or both. The look is chosen in the draft, seen in
the preview, and frozen into the version at publish. Each slice below
ships on its own, leaves every existing page and test as it was, and is
reviewed with `make gallery` against the style guide before it is
committed (CONTRIBUTING.md, "Building a page").

Migration numbers are the next free ones at the time of writing and move
up if another change lands first.

## Slice 1: the colour (size M)

**Goal.** A creator picks one of the ten Voice colours on the editor,
sees it in the preview, publishes, and respondents see it.

**Style guide first.**

- `docs/style-guide.md` gains "A survey's colour": the band at the head
  of a respondent's page, the selected choice and the progress use the
  survey's colour; it never carries text, never replaces the filled
  button, links, focus or Signal.
- Ten `--brand-fill-N` tokens and ten `--brand-on-N` tokens, set in
  `:root` and reassigned in the dark block, each measured and added to
  the contrast table. Voice 7 and 8 are deepened for the light card,
  Voice 1 is lifted for the dark card, to 3:1 or better; `--brand-on-N`
  is Ink or white, whichever holds 3:1 on its fill.
- `web/static/css/app.css`: `.brand-1` to `.brand-10` set `--brand` and
  `--brand-on` from those tokens, once, outside the theme blocks, so a
  forced theme (issue #15) needs nothing more. `.respond` rules that
  draw the band, `.option input:checked` and the progress read `--brand`
  and fall back to `--accent` when no class is set.

**Draft and version.**

- `internal/domain/survey.go`: `Draft` gains `Brand *Brand`
  (`json:"brand,omitempty"`) with `Voice int` (0 means the colour
  worked out from the ID). Validation refuses anything outside 0 to 10.
  A draft whose brand differs from the latest version's is a changed
  draft, so the editor offers Publish.
- Migration `NNNNN_version_brands.sql`: `version_brands(version_id uuid
  PRIMARY KEY REFERENCES survey_versions, voice smallint CHECK (voice
  BETWEEN 1 AND 10))`, with the `reject_mutation_of_published` trigger
  for UPDATE and DELETE. A version with no row has no chosen look.
- `db/queries/surveys.sql`: `CreateVersionBrand`, `GetVersionBrand`;
  `ListSurveysForWorkspace` returns the draft's chosen colour.
- `internal/store/surveys.go` `Publish` writes the row in the publish
  transaction when the draft has a brand.

**Pages.**

- `web/templates/surveys.templ`: a "Look" card on the editor with ten
  radio swatches and "Worked out for you", a plain form posting to
  `POST /surveys/{id}/brand` (`internal/http/surveys.go`), saved through
  `SaveDraft` like any draft change, so it appends a Draft Revision.
- `web/templates/respond.templ`: `RespondLayout` takes the brand and
  puts `brand-N` on `<body>`; `RespondData` carries it from the served
  version, the preview from the draft. `RespondThanks` takes the brand of
  the version the response was pinned to.
- `web/templates/views.go`: `SurveyView` gains `ChosenVoice`;
  `VoiceIndex` returns it when set; `CardVoices` moves only colours that
  were worked out.
- Wording in `web/text/active.en.toml` and `active.es.toml`: the card's
  heading, its one line of help, "Worked out for you", the name of each
  colour as the swatch's label, and the notice after saving.

**Export and purge.**

- `internal/export/export.go`: `FormatVersion = 3`; each version gains
  `"brand": {"voice": 3}`, omitted when none. `docs/export-format.md`
  documents it and adds a row to its Changes table.
- `internal/purge/purge.go`: delete `version_brands` for doomed surveys
  before `survey_versions`.

**Tests at the edge** (docs/testing.md).

- `internal/http/brand_test.go`: choosing a colour shows it in the
  preview and not on `/s/{id}`; after publishing, `/s/{id}` carries it
  and the thanks page does too; a response pinned to version 1 thanks
  in version 1's colour after version 2 changes it; a colour of 11 or a
  word is refused with a message and the draft is unchanged; a survey
  from another workspace cannot be branded.
- The dashboard card shows a chosen colour, and two neighbours that chose
  the same colour both keep it.
- `internal/purge/purge_test.go`: `version_brands` joins the list of
  tables a purged survey leaves empty.
- `internal/http/export_workspace_test.go`: format version 3, the brand
  round trips, a version without one has no `brand` key.
- The existing first party test and the ADR-0001 trigger tests stand.
- `e2e/tests/smoke.spec.ts`: a branded respondent page is axe clean in
  both themes.

**Gallery** (`e2e/gallery/gallery.spec.ts`): the editor's Look card with
nothing chosen and with a colour chosen; a respondent's page and thanks
page in Voice 7 (the lightest) and Voice 1 (the darkest in the dark
theme); the dashboard with a chosen colour.

## Slice 2: an image at the start (size L)

**Goal.** A creator uploads an image that respondents see above the
survey's title.

**Storage.**

- Migration `NNNNN_survey_images.sql`: `survey_images(id uuid PRIMARY
  KEY, survey_id uuid NOT NULL REFERENCES surveys, sha256 bytea NOT
  NULL, content_type text NOT NULL CHECK (content_type IN ('image/png',
  'image/jpeg')), width int, height int, size_bytes int NOT NULL CHECK
  (size_bytes <= 1048576), bytes bytea NOT NULL, created_at timestamptz,
  UNIQUE (survey_id, sha256))`. `version_brands` gains `start_image_id`
  referencing it and `start_alt text`.
- A new package `internal/brandimage` (domain logic, no HTTP): read the
  config with `image.DecodeConfig`, refuse over 2000 pixels a side or an
  unknown type, decode, encode again (PNG stays PNG, JPEG at quality 85),
  refuse if the result is over 1 MB. Only the standard library.
- `internal/store`: `SaveSurveyImage` refuses an eleventh image for a
  survey; `GetPublishedImage(sha)` returns an image only if a published
  version of a survey that is not deleted refers to it;
  `GetDraftImage(workspace, survey, sha)` for the preview.

**Routes** (`internal/http/routes.go`).

- `post("/surveys/{surveyID}/brand/image")`: multipart upload with the
  CSRF field first in the form; the existing 4 MB body cap stands and the
  handler reads at most 1 MB of the file. The draft records the image's
  hash and its alternative text, which is required.
- `post("/surveys/{surveyID}/brand/image/remove")`.
- `GET /brand/{sha256}`, public, and `get("/surveys/{surveyID}/brand/{sha256}")`
  for the preview. Both set the exact content type, `nosniff` (already
  set), and `Cache-Control: public, max-age=31536000, immutable` (the
  creator route `private`), replacing the `no-store` from
  `SecurityHeaders`. Neither writes a counter.

**Pages.** The Look card gains a file field, the current image with its
alternative text, and Remove. `Respond` draws the image on a light plate
above the `<h1>` with `width` and `height` attributes so the page does
not jump. `RespondUnavailable` shows it too when the survey has a
version, so a closed survey is recognisable.

**Export and purge.** The archive gains `images/<sha256>.<ext>`, once per
image however many versions use it; a version's `brand` gains
`"start_image": {"file": "images/….png", "alt": "…"}`. Images count to
the 64 MB cap. Purge removes `survey_images` with the survey, after
`version_brands`, and removes an image no version and not the current
draft refers to once it is seven days old.

**Tests at the edge.**

- Upload a PNG from `testdata/`, preview shows it, `/s/{id}` does not
  until publish, then does; the served bytes decode and carry no EXIF
  from a JPEG fixture that has a GPS tag; an SVG, a GIF, a 3000 pixel
  image, a 2 MB file and a file named `.png` holding text are each
  refused with a message; an upload without alternative text is refused.
- `GET /brand/{sha}` is 404 for a hash only a draft refers to, for a
  deleted survey's image, and for another workspace's draft image via
  the creator route.
- Opening the image does not change the survey's opened count
  (`/surveys/{id}/stats`).
- The respondent page's external origin test passes with an image.
- Purge and export as in slice 1, plus the orphan rule with the fake
  clock.

**Gallery.** The Look card with an image; a respondent's page with a
wide logo and with a tall one, at 390px and 1280px, in both themes; a
closed survey's page with its image.

## Slice 3: an image at the end (size S)

**Goal.** The thanks page shows the creator's end image in place of the
happy owl.

`version_brands` gains `end_image_id` and `end_alt` (migration
`00020`). The Look card gains a second file field. `RespondThanks`
draws the image where the owl was; with no end image the owl stays.
The preview's submitted page shows it too. The style guide's owl table
says the thanks page shows the owl unless the survey has an end image.
Tests: the thanks page after publish shows the end image and no owl; a
survey with only a start image keeps the owl. Gallery: the thanks page
with and without an end image, both themes.

## Not in these slices

- Any colour by hex value, which ADR-0018 leaves for later.
- A workspace brand that new surveys start from: issue #19 (creator
  preferences), which would copy a colour and images into a new draft and
  needs no change to how a survey is rendered.
- Hiding the footer, Earful's name or the owl from a respondent's page.
- Fonts chosen by the creator.

## Open questions for the owner

1. **Palette or free colour.** Is a choice of the ten Voice colours
   enough for the creators asking, or is a brand hex value the point? If
   it is, a second ADR covers a per survey stylesheet and adjusted
   colours, and slice 1 still ships first.
2. **One colour or two.** The issue says "colours". One accent is
   proposed; a second (for the band, say) doubles the measured pairs.
3. **Versioned look.** Changing only the logo makes a new version, which
   shows in the version list and the Audit Log. Is that acceptable, or
   should the look be a live setting like the title, at the cost of the
   preview and of knowing what a respondent saw?
4. **Where the colour goes.** Band, selected choice and progress are
   proposed. Should it also colour the submit button, which today is the
   page's one Ink filled button on every survey?
5. **Dark theme logos.** A light plate behind every image is proposed.
   Should a creator be able to upload a second image for dark?
6. **Limits.** 1 MB, 2000 pixels a side, ten stored images per survey,
   PNG and JPEG only. WebP needs a module outside the standard library;
   is it worth one?
7. **Alternative text in other languages.** It is in one language, as
   the title is (ADR-0015 notes the same of the title). Should it be part
   of a Localization?
8. **The thanks page.** Should the end image replace the owl, or sit
   beside it? Should a closed survey's page show the start image?
9. **Impersonation.** Does the runbook's support process cover a survey
   using someone else's logo, and do the terms need a line before this
   ships?
10. **Self hosted defaults.** Should an operator be able to switch
    branding off for their instance?
