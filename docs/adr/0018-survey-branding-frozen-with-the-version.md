# A survey's look is chosen from the palette and frozen with its version

Status: proposed. Would amend ADR-0016 and extend ADR-0001 and ADR-0010.

A creator can give a survey a look of its own: one colour, chosen from
the ten Voice colours, and an image above the questions, an image on the
page that thanks the respondent, or both. The choice is made in the
draft, shown in the preview, and frozen into the Survey Version at
publish, so a respondent sees the look of the version they were served.
Images are uploaded, never linked, and are stored in Postgres beside the
survey, re-encoded on the way in, and served from Earful's own origin.
Nothing in the Content Security Policy changes.

## Context

Every survey already has a colour, hashed from its ID onto the ten Voice
colours (`SurveyView.VoiceIndex` in `web/templates/views.go`). It shows
only on the creator's dashboard, as a stripe down the survey's card.
ADR-0016 says a stored, chosen colour can replace it if creators ask,
and lists a colour column on surveys as the option it did not need yet.
Creators have asked: a survey sent on behalf of an organisation should
look like it comes from that organisation.

What constrains the answer:

- **The policy is first party and has no inline style.**
  `internal/http/security.go` sends `style-src 'self'` and
  `img-src 'self' data:`. A colour cannot be written into a `style`
  attribute, and an image cannot be fetched from a creator's own site.
  Respondent pages load nothing from a third party (ADR-0006), and a
  build time test fails on any external origin in a respondent page
  (`internal/http/respond_test.go`).
- **Every page works without JavaScript.** Choosing a colour and
  uploading an image are plain forms.
- **The design is measured.** The style guide lists every colour pair
  with its contrast in both themes, and the axe gate runs on every page
  in both. A colour nobody has measured is a page nobody has reviewed.
  Measured against the card, Voice 7 (2.13:1) and Voice 8 (2.62:1) are
  below 3:1 on white, and Voice 1 (2.87:1) is below it on the dark card.
  Six of the ten carry neither white nor Ink text at 4.5:1. The Voice
  colours as they stand are stripes, not fills.
- **What a respondent was shown is immutable** (ADR-0001). Localizations
  were brought under that rule by freezing them at publish (migration
  00014); the look of the page is part of what a respondent was shown.
- **There is no object storage for application data.** Export archives
  live in Postgres so that a self hoster running `docker compose up` has
  the same feature as the hosted service (ADR-0010). The only bucket is
  the retention locked backup bucket (ADR-0008), which the application
  cannot write to.
- **Data stays in the EU and leaves with its owner.** Cloud SQL is in an
  EU region; anything stored there is covered by the existing processor
  list. The workspace export is the leaving promise, with a versioned
  format (`docs/export-format.md`), and the purge job is the only path
  that erases published data.
- **Anonymity is strong** (ADR-0003). Fetching an image must not become a
  way to count, identify or follow a respondent.

## Decisions

**One colour, from the Voice palette.** The creator picks one of the ten
Voice colours, or leaves the survey on the colour its ID gives it. Each
colour gets a measured pair per theme in the style guide: a fill that
holds 3:1 on the card, deepened in the light theme or lifted in the dark
one where the stripe colour does not, and the mark drawn on it (Ink or
white) at 3:1. A respondent page carries a `brand-N` class on its body;
the class sets `--brand` and `--brand-on` from tokens the theme blocks
define, so it needs no rule of its own for either theme, and a theme
chosen by attribute (issue #15) works with no further change. The
colour is a band at the head of the page, the selected choice, and the
progress through the questions. It never carries text, and it never
replaces the filled button (Ink, the page's one main action), links and
focus (teal, so a respondent finds them where they are on every survey),
or Signal, which keeps meaning that a microphone is open.

**Images are uploaded, re-encoded and served first party.** A creator
uploads a PNG or a JPEG through a multipart form. The server reads its
dimensions before decoding it, refuses anything over the size and pixel
limits, decodes it with the standard library and encodes it again. What
is stored is the re-encoded image, which carries no metadata: a phone
photograph's location does not reach a respondent. SVG is refused, since
an SVG is a document that can carry script and reference other origins.
Each image is a row in `survey_images`, owned by the survey, holding the
bytes, the type, the dimensions and a SHA-256 of the bytes. It is served
at `/brand/{sha256}` only while a published version of a survey that has
not been deleted refers to it, and at `/surveys/{id}/brand/{sha256}`, in
the creator's session, while the draft does. The address is the content,
so the response is cached as immutable, which overrides the `no-store`
that `SecurityHeaders` sets on dynamic pages. The handler counts nothing,
logs no more than any static file, and is never rate limited into the
abuse log.

**The look is part of the version.** The draft's JSON gains a `brand`
object (colour, start image, end image, and each image's alternative
text). Publishing copies it into `version_brands`, one row per version,
guarded by the trigger that guards questions. The respondent's page uses
the version it serves; the thanks page uses the version the response was
pinned to; the preview uses the draft. A creator who changes only the
look publishes a new version, which the editor already offers whenever
the draft differs from what is live.

**Images stay in Postgres.** The reasons in ADR-0010 hold with more
force: an image must be served by the application anyway, since the
policy admits no other origin, so a bucket would save database bytes and
nothing else, and would cost a self hoster a storage service to run a
survey with a logo. The limits make the cost bounded and stated: 1 MB and
2000 pixels a side per image, ten stored images per survey, two in use
per version.

**The dashboard shows the chosen colour.** A survey with a chosen colour
shows it on its card. The rule that moves a card's colour away from its
neighbour's applies only to colours worked out from the ID; a creator's
choice is kept.

## Considered Options

- **Any colour, with contrast worked out on the server.** The policy
  allows a per survey stylesheet from Earful's own origin, so a hex value
  could be served as `/s/{id}/brand.css` and adjusted until it holds its
  ratios in both themes. Rejected for now: each creator's colour would be
  a page no reviewer has seen, the adjusted colour would not be the one
  the creator typed, and the dark theme would be derived per colour
  rather than once. The stored shape below leaves room for it.
- **A colour column on surveys, outside the version.** As the title and
  the close date are. Simpler, and no republish to change a logo. It
  would mean a respondent reloading half way through could see a
  different page, the preview could not show a look before it went live,
  and an export could not say what any respondent saw.
- **A Cloud Storage bucket for images**, provisioned in
  `deploy/opentofu`. Unbounded and cheap per byte. It adds a bucket, IAM,
  a lifecycle rule and a second retention story, and the images would
  still be proxied through the application to stay first party. A self
  hoster would need one to brand a survey.
- **Images linked by address.** Nothing to store. A third party request
  from every respondent's browser, which ADR-0006 exists to prevent.
- **Images as `data:` URLs in the page.** The policy allows them and no
  route is needed. Every page view carries the whole image, it cannot be
  cached, and the HTML of the respondent's page grows by a megabyte.
- **Images owned by the workspace.** Reuse across surveys without a
  copy. Purge then needs a reference count across surveys and drafts, and
  a workspace level brand belongs with creator preferences (issue #19),
  which can copy an image into a survey when a survey is made.
- **A second image for the dark theme.** Faithful to logos drawn for
  dark grounds. Two uploads for every image; instead an image sits on a
  light plate in both themes, which reads correctly for the logos most
  organisations have.

## Consequences

- ADR-0016's survey colour becomes a default: a survey has its hashed
  colour until its creator chooses one. The style guide gains the brand
  fill tokens, their measured pairs in both themes, the band, and the
  rule that the brand colour never carries text.
- The owl's Happy mood on the thanks page gives way to the creator's end
  image where there is one; the page has one picture, not two. The
  footer keeps the owl, Help and the privacy page on every survey, so a
  respondent always knows what they are answering on.
- The workspace export moves to format version 3: each version carries a
  `brand` object, and the archive gains an `images/` folder named by
  hash. Images count toward the 64 MB cap. Version 2 fields are unchanged.
- The purge job deletes `version_brands` and `survey_images` with the
  survey, before the versions they refer to. An image referenced by no
  version and not by the current draft is removed after seven days, so
  replaced uploads do not accumulate.
- Images are in the database, so they are in the daily dumps for the 30
  days ADR-0008 keeps them. Erasure of an image is complete when the last
  dump holding it expires, as it is for every other row.
- A logo makes a survey look like it comes from whoever owns that logo,
  including somebody who does not. The respondent's page already names
  the workspace as the controller of the answers; the abuse path for a
  survey impersonating an organisation is a support matter, recorded in
  the runbook, and the terms say a creator may only use marks they are
  entitled to.
- No new processor, no new infrastructure, no change to the policy, and
  nothing a self hoster must configure.
- The anonymity of a respondent is unchanged: the image route keeps no
  record of who fetched what, and an image request is not an open.
