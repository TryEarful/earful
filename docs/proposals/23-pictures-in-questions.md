# Pictures in questions: build plan

Issue #23. Decision: [ADR-0021](../adr/0021-pictures-in-questions-stored-in-postgres.md)
(Proposed). Each slice below ships on its own, passes `make check` and
`make e2e-smoke`, and leaves every existing survey reading back exactly
as before. Tests drive the application over HTTP as
[docs/testing.md](../testing.md) describes; the only tests below the
edge are the ones that file already allows (immutability in raw SQL)
and the pure image pipeline, which has no edge of its own until slice 2.

Migration numbers are written as "next": sibling proposals may land
first.

## Slice 1 · The media store (M)

Infrastructure only; nothing a creator can see yet.

- `internal/media`: `Process` (sniff, pixel budget, decode, EXIF
  orientation, resize to 1600 px and 640 px, re-encode JPEG or PNG),
  `Store` interface and its Postgres implementation, a decode
  semaphore, `Limits` with the caps from the ADR.
- `go.mod`: `golang.org/x/image` (draw, webp).
- Migration next: `media` (id uuid, workspace_id, survey_id null,
  purpose, content_type, width, height, bytes, sha256, full bytea,
  card bytea, uploaded_by, created_at) with the
  `reject_mutation_of_published` trigger; `version_media (version_id,
  media_id)` with the same trigger; a unique index on
  `(workspace_id, sha256)` so the same picture uploaded twice is stored
  once.
- `db/queries/media.sql` through sqlc.
- `internal/http/server.go`: `limitBody` lets the upload route apply its
  own cap instead of `maxRequestBytes`, by an explicit list of one
  route, with a comment saying why.
- `internal/http/security.go`: `SecurityHeaders` leaves `Cache-Control`
  to the media handler for `/media/`.
- `internal/purge/purge.go`: steps for `version_media` and `media` of
  doomed surveys and Workspaces, and for unreferenced pictures older
  than a day.
- Tests:
  - `internal/media/process_test.go` (pure function): a JPEG with an
    EXIF block holding GPS coordinates comes out with no APP1 segment
    and none of the coordinate bytes; orientation 6 comes out rotated;
    SVG, HTML, a PNG with trailing script, a declared 100000 × 100000
    canvas and a truncated file are each refused with their own error.
  - `internal/store/immutability_test.go`: UPDATE and DELETE on `media`
    and `version_media` refused outside a purge.
  - `internal/purge/purge_test.go`: the schema test now finds `media`
    and `version_media` and passes only with the new steps.

## Slice 2 · A picture on a question (M)

- `internal/domain/question.go`: `Picture {MediaID, Alt}`,
  `Question.Image *Picture`; `Validate` refuses a picture without alt
  text or with more than 250 characters of it. Messages go through
  `LimitError` and the existing error wording.
- Migration next: `questions.image jsonb` null.
- `db/queries/surveys.sql`: publish writes `image` and the
  `version_media` rows in the publish transaction.
- Routes in `internal/http/routes.go`: `POST /surveys/{surveyID}/media`
  (upload, returns to the editor with the picture attached to the
  question named in the form), `POST
  /surveys/{surveyID}/questions/{identityID}/image/remove`, `GET
  /media/{id}` (serving rule from the ADR).
- `web/templates/surveys.templ`: per question, a multipart form with a
  file input (`accept="image/jpeg,image/png,image/gif,image/webp"`), an
  alt text field and an outlined button; the picture shown above it
  with its alt text editable in the draft form. No script is needed;
  `web/static/js` may later show the chosen file before it is sent.
- `web/templates/respond.templ`: the picture between the question and
  its answers, with `width`, `height`, `alt`, `loading="lazy"` after the
  first question. Same template serves the preview.
- `web/templates/results.templ`: the picture beside the question, and a
  note where it changed between versions.
- `web/text/active.en.toml`, `active.es.toml`: `editor.picture.*`
  (add, alt label, alt hint, remove, too large, wrong kind, too many
  pixels, quota reached, missing alt), sentence case, no dashes.
- `docs/style-guide.md`: a `.picture` component (radius and border
  from the tokens, no shadow, never wider than the card).
  `web/static/css/app.css` in the same change.
- A survey with Localizations shows the source alt text in every
  language until slice 4 lands; the editor says so beside the field.
- Tests in `internal/http/media_test.go` and `surveys_test.go`:
  upload as a creator, publish, fetch from an anonymous client: 200
  with `image/jpeg`, `nosniff`, `default-src 'none'`; a draft only
  picture fetched anonymously is 404; another Workspace's session gets
  404; a 9 MB upload is accepted and a file over the cap is refused
  with the message; publishing with empty alt text is refused; the
  respondent page has an `<img>` with the alt text and a first party
  `src` (the existing third party guard in `respond_test.go` covers
  the rest); after delete and purge the address is 404.
- e2e: `e2e/tests` uploads a fixture from `testdata` through the file
  input with JavaScript off and on, answers the survey, and runs axe.
- Gallery (`e2e/gallery/gallery.spec.ts`): editor with no picture,
  with a picture, with each upload error; respondent question with a
  picture; preview; results with a picture; in both themes and both
  languages as the gallery always does.

## Slice 3 · Pictures on options (L)

- `internal/domain`: `Option {Label, Image *Picture}` replacing
  `[]string`, with `MarshalJSON` writing a plain string when there is
  no picture and `UnmarshalJSON` reading either form. `ValidateAnswer`,
  `containsOption`, `canonicalAnswers`, the results fold, the CSV
  writer and the export read `Label`. Rule: every option has a picture
  or none does.
- The options textarea stays the way labels are written. Pictures are
  attached on a page of their own per question,
  `GET /surveys/{surveyID}/questions/{identityID}/pictures`, one row per
  option with a file input and alt text, so the editor's main form does
  not become a multipart form.
- `web/templates/respond.templ`: option cards in a grid (one column at
  390 px, up to three at 1280 px), the card rendition, the input named
  by the label and described by the alt text, key hints unchanged.
- `web/templates/results.templ`: the card beside each option's count.
- Style guide: `.option-card` as a variant of `.option`, with its
  selected state in Deep Teal, not Signal.
- Tests: a draft written before this slice (options as strings) opens,
  saves and publishes unchanged, compared byte for byte; a published
  version from before reads back the same through the results page and
  CSV; an answer to a picture option stores the label; a question with
  pictures on some options only is refused; the keyboard path (story
  80) selects a card by letter.
- Gallery: picture options single and multiple, nothing selected, one
  selected, an error for a required question; the pictures page empty,
  half done, complete.

## Slice 4 · Alt text in every language (M)

- `internal/domain/localization.go`: `LocalizedQuestion` gains
  `ImageAlt`, `OptionAlts` and the source alt each was made from;
  `MarkStale` treats a change to any source alt as a change to the
  source.
- `question_localizations.options` carries localized option alts in the
  object form; migration next adds `question_localizations.image_alt
  text` null.
- The AI translation call includes alt text, under the existing meter
  (the guard test in `ai_meter_guard_test.go` must stay green).
- `web/templates/localizations.templ`: alt text rows beside each
  question's and option's translation, with the picture shown small for
  context.
- Tests: `?lang=es` serves the Spanish alt; rewording the English alt
  makes the Spanish unreviewed and publishing is refused until it is
  read; a survey published before this slice serves unchanged.
- Gallery: the localization page with pictures, reviewed and stale.

## Slice 5 · Export version 3 (S)

- `internal/export`: `image` and `option_images` in `workspace.json`,
  files under `media/` in the zip, `format_version: 3`, `README.txt`
  updated.
- `docs/export-format.md`: version 3 in the format and in "Changes".
- Tests in `export_workspace_test.go`: each exported file's SHA 256
  matches the JSON; a survey without pictures exports as before but
  for the version number; a Workspace over the cap fails with the
  existing message.

## Slice 6 · What the instance tells people (S)

- `web/pages/trust.en.md`, `trust.es.md`: pictures are stored in the
  database in the instance's region, metadata removed, deleted with the
  survey and gone from backups within the backup window. `make pages`.
- `web/pages/help.*.md`: how to add a picture, why alt text is
  required, what happens to the file.
- No new sub-processor; the table in PLAN.md Appendix B is unchanged.

## Not in this plan

- **Pictures or video from respondents** (issue #21). Needs its own
  ADR: anonymity of what a respondent's file contains, abuse of an
  anonymous upload, unlawful content, and video transcoding. It would
  reuse `internal/media`.
- A picture that differs per language.
- Cropping or editing a picture in Earful.
- AI generated pictures (SPEC.md lists them as out of scope).
- A picture on a yes or no, scale or text question's answers.

## Open questions for the owner

1. **Upload cap.** 10 MB for the one upload route, with the 4 MB cap
   kept for every other route? A lower cap refuses many phone photos
   on the no script path; a script could shrink a photo before sending
   it, but the no script path must work regardless.
2. **Workspace quota.** 50 MB of stored pictures per Workspace? It sets
   how many pictured surveys fit and keeps exports under 64 MB. Is the
   quota the same for every Workspace, or a setting per plan later?
3. **Picture count per survey.** A cap (say 30) in addition to the
   quota, to keep a respondent's page light on a phone?
4. **Mixed options.** The ADR requires a picture on every option or on
   none. Is a question with a picture on some options wanted?
5. **Changing a picture in a published survey.** The proposal keeps the
   Question Identity and shows the change in results, like a reworded
   question. Should replacing an option's picture instead be treated as
   a new option, since "which do you prefer" over different pictures is
   arguably a different question?
6. **Enlarging a picture.** On a phone a card is small. A "view larger"
   link opens the full rendition on its own page, which without
   JavaScript leaves the form. Acceptable, or only with a script?
7. **Caching.** `private, max-age=86400` on pictures. A picture removed
   from a survey may stay in a respondent's browser cache for a day.
   Acceptable?
8. **Shared store with ADR-0018.** Whichever of this and the logo work
   lands first creates `internal/media` and the `media` table. Does the
   logo need SVG? This pipeline refuses SVG, and accepting it would need
   a decision of its own.
9. **Respondent uploads (#21).** Confirm that receiving pictures or
   video from respondents is a separate, later decision and not part of
   this issue.
