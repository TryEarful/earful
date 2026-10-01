# Pictures in questions are re-encoded on upload and stored in Postgres

Status: Proposed

A creator can put a picture on a question, and on each option of a
single choice, multiple choice or dropdown question, so that a
respondent can be asked which of the things shown they prefer. Every
picture has alt text, written by the creator and required before the
draft can be published.

A picture is decoded on upload and written out again by Earful's own
encoder, at a bounded size, as JPEG or PNG. Nothing the creator sent is
stored or served: not the original bytes, not EXIF, XMP or IPTC
metadata, not a colour profile, not a GPS position. What is stored is a
row in a `media` table in Postgres, owned by a Workspace, immutable once
written, and served by the application at `/media/{id}`. Respondent
pages therefore load pictures from Earful's own origin, under the
Content Security Policy as it is (`img-src 'self' data:`), and no new
processor, bucket or configuration is needed on any instance.

The storage sits behind a small interface in a new package,
`internal/media`, which ADR-0018 (a workspace's logo) can use as it
stands: a sanitizing pipeline that turns an upload into a stored
picture, a store that keeps and returns it, and one route that serves
it. Options stop being bare strings: an option is a label and, when it
has one, a picture with its alt text.

## Context

- **No object storage exists, on purpose.** ADR-0010 keeps workspace
  exports in Postgres so that the SaaS and a `docker compose up`
  instance run one code path. The same argument applies here with more
  force: a survey with pictures that renders on one kind of instance
  and not the other is a broken survey, not a missing feature.
- **Respondent pages are first-party only** (ADR-0006, the CSP in
  `internal/http/security.go`). A picture hotlinked from another site,
  or served from a storage provider's domain, is a request from a
  respondent's browser to a third party that learns their IP address.
- **Anonymity is strong** (ADR-0003). A picture is shown to a
  respondent, never received from one, so the response path is
  unchanged; but nothing about fetching a picture may identify who
  fetched it. Picture addresses are the same for every respondent and
  carry nothing per request.
- **Published versions are immutable** (ADR-0001), by trigger on
  `questions` and `question_localizations`. A picture that a published
  version shows has to be as immutable as its wording, and has to
  outlive every later edit of the draft.
- **Every save of a draft appends a revision** (`draft_revisions`), a
  full copy of the draft's JSON kept for 90 days. Anything stored inside
  the draft's JSON is copied on every autosave.
- **Every feature works without JavaScript** (CONTRIBUTING.md). A
  picture is chosen with a file input in an ordinary form.
- **Every request body is capped at 4 MB** (`maxRequestBytes` in
  `internal/http/server.go`). A photo from a phone camera is often
  larger.
- **Answers to choice questions store the option's label** in the
  survey's source language (`domain.AnswerValue.Choice`, mapped back
  from a Localization by position in `canonicalAnswers`). Results, CSV
  and the export are built on that label.
- **The axe gate fails the build** on an image without a text
  alternative, and a picture that carries the question's meaning needs
  a real one, not `alt=""`.
- **A Localization is reviewed before it is published** (story 23). Alt
  text is wording a respondent reads, through a screen reader.
- **The workspace export is a versioned contract**
  (`docs/export-format.md`) capped at 64 MB (ADR-0010).
- **Purge reaches every table under a survey**, and a test reads the
  schema to prove it (`internal/purge/purge_test.go`).

## Decision

**Storage.** A `media` table holds each picture's processed bytes
(`bytea`), its content type, dimensions, byte size, the SHA 256 of the
stored bytes, the owning `workspace_id`, the `survey_id` it was
uploaded for (null for a picture that belongs to the Workspace, such as
a logo), a `purpose` (`question` here; ADR-0018 adds its own), and who
uploaded it and when. Rows are immutable by the same trigger that
guards published questions: a picture is never edited, only replaced
by a new row. The purge job is the only deleter.

**The interface.** `internal/media` exposes:

- `Process(r io.Reader, Limits) (Picture, error)`: sniff, bound,
  decode, orient, resize, re-encode. Pure, no storage, tested by
  feeding it hostile files.
- `Store` with `Put`, `Get` and `Delete`, implemented over Postgres.
  The handlers depend on the interface, so an object store in the EU
  can be put behind it later without touching them, provided pictures
  are still served through the application (see Considered Options).
- One serving handler for `GET /media/{id}`, shared by every purpose.

**Upload.** `POST /surveys/{surveyID}/media`, CSRF protected, takes one
file in a multipart form. The 4 MB request cap stays for every other
route; this route alone gets its own, larger cap (proposed 10 MB),
applied in place of the global one. The pipeline:

1. Read at most the cap; sniff the first 512 bytes with
   `http.DetectContentType` and accept only `image/jpeg`, `image/png`,
   `image/gif` and `image/webp`. SVG is refused: it is a document that
   can carry script, and re-encoding it is rasterizing it, which Go's
   standard library cannot do. HEIC has no Go decoder; browsers on
   iOS convert it to JPEG when the file input asks for JPEG or PNG.
2. `image.DecodeConfig` before decoding, refusing anything above a
   pixel budget (proposed 40 megapixels), so a small file that
   declares an enormous canvas is refused before it is allocated.
   Decodes run under a semaphore so concurrent uploads cannot exhaust
   an instance's memory.
3. Decode; apply the EXIF orientation tag to the pixels, so a phone
   photo is upright once its metadata is gone; then drop everything
   that is not a pixel.
4. Resize so the long edge is at most 1600 px, and write a second
   rendition at most 640 px for option cards. JPEG at a fixed quality
   for opaque pictures, PNG for pictures with transparency. An
   animated GIF keeps its first frame.
5. Store. The served bytes are always the output of Go's encoders, so
   a polyglot file, trailing data or a crafted metadata block cannot
   survive into what a respondent's browser receives.

Resizing and WebP decoding use `golang.org/x/image`, the one new
dependency, from the same project as `x/crypto` and `x/text` already in
`go.mod`. Orientation is read by a short in-house parser of the one
EXIF tag, rather than a general EXIF library.

**Serving.** `GET /media/{id}?r=card|full` answers when the picture
belongs to a survey that is not deleted and is shown by one of that
survey's published versions, or when the request has a session in the
owning Workspace (the editor and the preview). Anything else is 404,
indistinguishable from a picture that does not exist. Responses carry
the stored content type, `X-Content-Type-Options: nosniff` (already set
globally), `Content-Disposition: inline`, a restrictive
`Content-Security-Policy: default-src 'none'`, and
`Cache-Control: private, max-age=86400`, since the global `no-store`
would refetch every picture on every question. The id is a random UUID,
not the content hash, so equal pictures in two Workspaces cannot be
recognised from their addresses.

**The question model.** `domain.Question` gains `Image *Picture`, and
`Options []string` becomes `Options []Option`, where an `Option` is a
`Label` and an optional `Image *Picture`; a `Picture` is a media id and
its `Alt` text. An option without a picture is marshalled as the plain
string it is today, and both forms are accepted when reading, so every
stored draft, revision and published `questions.options` row reads
back unchanged and none has to be migrated (they could not be: the
published rows are immutable). The label stays required and stays what
an answer stores, so results, the CSV and `canonicalAnswers` are
unchanged. A picture without alt text, or with alt text over 250
characters, fails `Question.Validate`, which publishing re-runs. Either
every option of a question has a picture or none does, so the
respondent sees one consistent grid.

**Publishing.** A new column `questions.image jsonb` (null when none)
carries the question's picture; option pictures travel inside
`questions.options`. Adding a nullable column fires no row trigger, so
existing published rows are untouched. Publishing also writes one row
per picture into `version_media (version_id, media_id)`, immutable like
the version, which is what the serving rule and the purge read rather
than searching JSON.

**Localization.** The picture is the same in every language; its alt
text is translated. `LocalizedQuestion` gains the alt text of the
question's picture and of each option's, with the source alt it was
made from, and a change to the source alt makes the translation
unreviewed, exactly as a change to the question's text does. The AI
translation that already drafts a Localization drafts alt text with
it, metered like the rest. A picture that differs per language (a
label printed in Spanish on packaging) is not part of this decision.

**Respondent page.** The question's picture sits between the question
and its answers. A choice question with pictures draws its options as
cards in a grid of one column on a phone and up to three on a desktop:
the picture, then the label, with the radio or checkbox and the key
hint (story 80) unchanged. The input is named by the label and
described by the picture's alt text (`aria-labelledby` and
`aria-describedby`), so a screen reader reads both, once each. Every
`<img>` has `width` and `height` from the stored dimensions so the page
does not shift while pictures load. The style guide gains the picture
and the option card as components.

**Results.** Each option shows its card rendition beside its count.
Where a picture changed between versions with the label kept, results
say so, as they do for a reworded question (ADR-0001).

**Limits.** Per picture, the upload cap above. Per Workspace, a total
of stored pictures (proposed 50 MB), refused with a message that says
how much is used, so that exports stay under ADR-0010's cap and the
database does not become free hosting.

**Purge.** A deleted survey's pictures go with it, after
`version_media` and before `surveys`. A picture that no draft,
retained revision or published version refers to, and that is older
than a day, is deleted, so that an upload abandoned in the editor does
not count against the Workspace forever. Erasing an account erases its
Workspace's pictures with the Workspace.

**Export.** Format version 3: each question may carry `image`, and
`options` stays an array of strings with a parallel `option_images`
array (null where an option has none), each entry holding the media
id, alt text, content type, dimensions, SHA 256 and the path of the
file in the archive, `media/<id>.<ext>`. Only the full rendition is
exported; the card is derived. Version 2 readers see every field they
knew, unchanged. Archives above 64 MB still fail with the message
ADR-0010 promises.

**Pictures and video sent by a respondent** (the second part of issue
#21) are not part of this decision. Receiving files on the response
path, from anonymous strangers, is a different problem: it puts
content a respondent made next to their answers, which may identify
them by face, place or metadata (ADR-0003), it turns an anonymous form
into free file hosting for bots, it brings obligations for unlawful
content an operator did not choose to hold, and video needs
transcoding that nothing in Earful can do. It needs an ADR of its own,
which can reuse `internal/media`'s pipeline.

## Considered Options

- **A bucket (GCS in an EU region) with signed URLs.** The usual
  answer, and unbounded. It is the option ADR-0010 rejected for the
  same reasons: a self-hosted instance either needs object storage
  configured or shows broken questions, and the trust page gains a
  bucket, IAM and a second retention story. A signed URL also puts a
  storage provider's domain into `img-src`, and a request to it from
  every respondent's browser, which ADR-0006 exists to prevent.
  Rejected; the `Store` interface leaves room for a bucket read through
  the application if database size ever forces it.
- **Pictures as `data:` URIs inside the draft's JSON.** No table, and
  the CSP already allows `data:`. But the draft is copied into a
  revision on every autosave and kept 90 days, so a survey with six
  pictures would store them hundreds of times; every page would carry
  every picture inline, uncached; and the export would hold them as
  base64 in `workspace.json`. Rejected.
- **A creator pastes a picture's address.** No storage at all. Every
  respondent's browser would then fetch from a host the creator chose,
  which sees their IP address and can change the picture after
  publishing, breaking ADR-0006 and ADR-0001 at once. Rejected.
- **A disk volume.** Cloud Run has no persistent disk, and a volume is
  a second thing to back up on every other host. Rejected.
- **Storing the upload as sent and stripping metadata in place.**
  Smaller change, faster upload. Stripping is a parser per format that
  has to know every place metadata can hide, and serving what a
  stranger's file contained is how polyglots reach a browser.
  Re-encoding removes both problems by construction. Rejected.
- **Option pictures as a parallel array beside `options` in the
  model.** Fewer changes to the code that reads options. But reordering
  or removing an option in the editor would have to move two arrays in
  step, forever. Kept only in the export, where the arrays are a
  snapshot and an importer benefits from `options` keeping its type.
- **Alt text optional, defaulting to the option's label.** Less to
  type. But the label names an option ("A") and says nothing of what is
  in the picture, which is the whole question for a respondent who
  cannot see it. Rejected.

## Consequences

- Database size grows with pictures, and so do the daily exports kept
  for 30 days (ADR-0008); a deleted picture is gone from backups only
  when they roll over, as every other deletion is. The trust page and
  the privacy notice say that pictures are stored in the database, in
  the instance's region, with their metadata removed.
- Uploads are slower than a plain store because of decoding and
  encoding, and use memory in proportion to the pixel budget. The
  semaphore trades throughput for a bound.
- A creator's picture is altered on upload: smaller, recompressed, its
  colour profile dropped. This is stated beside the file input.
- An option is no longer a string anywhere in Go. Every reader of
  `Options` changes in one commit; the JSON stays compatible, so no
  data does.
- The export format moves to version 3 and `docs/export-format.md`
  records it.
- The immutability test gains `media` and `version_media`; the purge
  schema test forces a purge step for each before it can pass.
- `img-src` is unchanged. The serving route is the first page response
  besides `/static/` that a browser may cache.

Amends ADR-0010 (pictures are a second kind of bytes in Postgres, under
a per Workspace quota that keeps exports under its cap) and extends
ADR-0001 (a picture shown by a published version is immutable with it).
Shares `internal/media` with ADR-0018.
