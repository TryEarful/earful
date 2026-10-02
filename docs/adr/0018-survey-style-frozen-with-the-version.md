# A survey's style is a theme, a header and a footer, frozen with its version

Status: accepted. Amends ADR-0016 and extends ADR-0001 and ADR-0010.

A creator can give a survey a style of its own: one of four themes that
Earful designs, a header (a banner, a logo, a name, a tagline and links),
a footer (text and links), and the picture on the page that thanks the
respondent. The style is set in the draft, shown in the preview, and
frozen into the Survey Version at publish, so a respondent sees the style
of the version they were served. Images are uploaded, never linked, and
are stored in Postgres beside the survey, re-encoded and scaled on the
way in, and served from Earful's own origin. Nothing in the Content
Security Policy changes.

## Context

Creators send surveys on behalf of organisations, and such a survey
should look like it comes from that organisation. Until now every
respondent's page looked the same, and the only colour a survey had was
the one hashed from its ID, shown as a stripe on the creator's dashboard
(ADR-0016).

What constrains the answer:

- **The policy is first party and has no inline style.**
  `internal/http/security.go` sends `style-src 'self'` and
  `img-src 'self' data:`. A colour cannot be written into a `style`
  attribute, and an image cannot be fetched from a creator's own site.
  Respondent pages load nothing from a third party (ADR-0006), and a
  build time test fails on any external origin in a respondent page
  (`internal/http/respond_test.go`).
- **Every page works without JavaScript.** Choosing a theme and
  uploading an image are plain forms.
- **The design is measured.** The style guide lists every colour pair
  with its contrast in both display modes, and the axe gate runs on
  every page in both. A colour nobody has measured is a page nobody has
  reviewed. An uploaded image is the one surface whose colours cannot be
  measured in advance.
- **Components use only semantic tokens.** `--bg`, `--surface`,
  `--text`, `--link`, `--button` and the rest are the only colours a
  component names, and the dark display mode reassigns only those
  (ADR-0016). A whole page can therefore be recoloured by reassigning
  one set of tokens, with no component rule changed.
- **Some colours carry meaning.** Signal coral means that a microphone
  is open, and the status colours mean good, warning and error. A
  respondent has to be able to rely on them on every survey.
- **What a respondent was shown is immutable** (ADR-0001). The thank you
  message and the Localizations were brought under that rule by freezing
  them at publish; the style of the page is part of what a respondent
  was shown.
- **There is no object storage for application data.** Export archives
  live in Postgres so that a self hoster running `docker compose up` has
  the same feature as the hosted service (ADR-0010). The only bucket is
  the retention locked backup bucket (ADR-0008), which the application
  cannot write to.
- **Data stays in the EU and leaves with its owner.** Anything stored in
  the database is covered by the existing processor list. The workspace
  export is the leaving promise, with a versioned format
  (`docs/export-format.md`), and the purge job is the only path that
  erases published data.
- **Anonymity is strong** (ADR-0003). Fetching an image must not become a
  way to count, identify or follow a respondent.

## Decisions

**Two words, two things.** A *theme* is the set of colours a creator
chooses for a survey. The *display mode* is light or dark, and belongs
to the reader. What ADR-0016 and the code called the theme (the cookie,
`data-theme`, the switcher) is renamed to mode, so that neither word has
two meanings in one stylesheet. The old `theme` cookie is still read, so
a reader's saved choice survives the rename.

**Four themes, designed by Earful.** Earful is the default and is the
page as it was. Slate is neutral and sits beside any logo; Ocean is
blue; Forest is green. A theme is a class on the respondent page's
`<html>` (`theme-slate`, `theme-ocean`, `theme-forest`), beside the
display mode, so that the ground behind the page is the theme's too. It
reassigns the semantic tokens: the page and card grounds, text and muted
text, links, focus and its ring, the accent, the filled button, the
borders, and the ground of a notice, which is a tint of the accent and
follows it. Each theme has a light and a dark
variant, and the respondent's display mode chooses between them: a
creator's choice never overrides it. Surfaces are tinted, never
saturated. Every pair is measured in all eight combinations and listed
in the style guide, and a test fails the build when a pair falls below
its threshold. More themes can be added the same way.

**What a theme may not change.** Signal and the status colours (good,
warning, danger) with their tints are the same on every theme, and are measured again on each
theme's surfaces. No theme uses a hue near Signal, so the record button
is the only coral on any page. The typeface, spacing, radii and shadows
are not part of a theme.

**The header keeps what is readable off the image.** The banner is a
strip across the head of the page, cropped to three to one, decorative,
with empty alternative text, and nothing is drawn on it. The logo sits
on a plate in the theme's surface colour, overlapping the banner's lower
edge, so it has a known ground in both display modes and needs no second
upload for dark. Below, on the theme's surface, are a name (one line, 80
characters), a tagline (280 characters, line breaks kept) and up to
three links, each a label and an absolute `http` or `https` address,
checked as the thank you link is (`internal/domain/thanks.go`). All of
it is plain text. Links open in a new tab with `noopener noreferrer`.
The survey's title stays the page's `<h1>`. Every part is optional and
none depends on another.

**The creator's footer sits above Earful's.** It shows the same logo,
small, a text of up to 280 characters and up to three links. Earful's
own footer stays on every survey: "Powered by Earful", Help, the privacy
page, the link to report the survey, and the display mode switcher. A
survey that has its own logo does not show the owl beside those words,
so a page has one mark; this is worked out from the style and is not a
setting.

**Every respondent page of the survey wears the style.** The questions,
the thanks page, the page for somebody who already answered, and the
page of a closed survey that has a published version all carry the
theme, the header and both footers. After the first page the header is
compact (the logo and the name, no banner), so a paged survey does not
push each question below the banner. A survey that was never published
has no frozen style and shows Earful's default.

**The thanks page has a picture the creator chooses.** The happy owl is
the default. The alternatives are three illustrations with no owl in
them (a check mark, an envelope, confetti), drawn inline and filled from
the theme's tokens as the owl is, an image the creator uploads, shown on
a plate, or no picture.

**Text is translated as the thank you message is.** The tagline, the
footer text, the link labels and the alternative text of the logo and of
an uploaded thanks picture are part of a Localization. They are drafted
and reviewed with the questions, and a survey that carries a language
is not published until they have been read in it, since an unreviewed
translation must not go out in a creator's name. The reviewed words are
frozen inside the version's `style`, under `localizations`. A respondent
reading a language the version has no translation into reads them as
written. The name and the link addresses are shared by every language.

**The style is part of the version.** The draft's JSON gains a `style`
object beside `thanks`. Publishing copies it into a `style` jsonb column
on `survey_versions`, where the trigger that already refuses UPDATE and
DELETE guards it, as it guards the thank you columns. The respondent's
page uses the version it serves; the thanks page uses the version the
response was pinned to; the preview uses the draft. A creator who
changes only the style publishes a new version, which the editor already
offers whenever the draft differs from what is live.

**Images are uploaded, re-encoded, scaled and served first party.** A
creator uploads a PNG, a JPEG or a WebP through a multipart form. The
server reads the dimensions before decoding, decodes, scales the image
down to its slot's size with `golang.org/x/image`, and encodes it again.
What is stored carries no metadata: a phone photograph's location does
not reach a respondent. SVG is refused, since an SVG is a document that
can carry script and reference other origins.

| Slot | Upload cap | Stored as |
|---|---|---|
| Banner | 2 MB | JPEG, at most 1600 pixels wide |
| Logo | 1 MB | PNG or JPEG, at most 512 pixels a side |
| Thanks picture | 1 MB | PNG or JPEG, at most 800 pixels a side |

Each image is a row in `survey_images`, owned by the survey, holding the
bytes, the type, the dimensions and a SHA-256 of the bytes, with at most
ten rows per survey. It is served at `/style-image/{sha256}` only while
a published version of a survey that has not been deleted refers to it,
and at `/surveys/{id}/style-image/{sha256}`, in the creator's session,
while the draft does. The address is the content, so the response is
cached as immutable, which overrides the `no-store` that
`SecurityHeaders` sets on dynamic pages. The handler counts nothing,
logs no more than any static file, and is never rate limited into the
abuse log.

**Images stay in Postgres.** The reasons in ADR-0010 hold with more
force: an image must be served by the application anyway, since the
policy admits no other origin, so a bucket would save database bytes and
nothing else, and would cost a self hoster a storage service to put a
logo on a survey. Scaling on the way in keeps the cost bounded and
stated.

**The style has a tab of its own.** The editor's tabs gain Style, at
`/surveys/{id}/style`: one multipart form with four sections (theme,
header, footer, thanks picture) and one filled button. An image is
removed with a checkbox beside it, not a second button. The theme is
chosen from four radio cards, each a small sample drawn in that theme's
tokens. Saving goes through `SaveDraft`, so it appends a Draft Revision
like any other draft change.

**A style that looks like somebody else's is guarded against.** A
banner, a logo, a name and links can make a page look like it comes from
an organisation that did not send it. Four things stand against that.
Earful's footer carries "Report this survey" on every respondent page,
which addresses a message to the instance's `CONTACT_EMAIL` with the
survey's address in it; an instance without one does not show the link.
The disclosure that names the workspace as the controller of the answers
stays above the questions in Earful's words, coloured by the theme and
not editable. The upload form says that only a logo the creator has the
right to use may be uploaded. The runbook has a procedure for a
complaint that a survey impersonates someone. The terms say that a
creator may only use names and marks they are entitled to.

**No switch for an operator.** The style needs no service and no
configuration, and its storage is bounded, so an instance has nothing to
turn off. An instance wide or workspace wide default style is a
different need and belongs with Survey Defaults (ADR-0019).

**The dashboard is unchanged.** A survey's card keeps the stripe hashed
from its ID. A theme is shared by many surveys and would not tell them
apart.

## Considered Options

- **One of the ten Voice colours as an accent.** The Voice colours are
  stripes, not fills: several fall below 3:1 on the card in one display
  mode or the other, and most carry neither white nor Ink text. Ten
  swatches is also more choice than a creator needs to sit beside a
  logo, and one accent on an otherwise fixed page does little to make a
  survey an organisation's own.
- **A theme that changes only an accent** (a band, the selected choice,
  the progress), leaving the grounds, the button and the links as they
  are. Cheaper to review. The page would still read as Earful's with a
  stripe on it, and the semantic tokens already make a whole surface as
  cheap to write as an accent.
- **Any colour, with contrast worked out on the server.** The policy
  allows a per survey stylesheet from Earful's own origin, so a hex
  value could be served and adjusted until it holds its ratios in both
  display modes. Each creator's colour would be a page no reviewer has
  seen, and the adjusted colour would not be the one the creator typed.
- **A theme with one fixed appearance,** dark or light for everyone.
  Half the token blocks. The display mode is the reader's setting, and
  some readers need it.
- **Text over the banner, behind a scrim.** Closer to a hero image. The
  contrast of the text would depend on the upload, which neither the axe
  gate nor a review can see in advance.
- **A second image for the dark display mode.** Faithful to logos drawn
  for dark grounds. Two uploads for every image; the plate gives one
  upload a known ground in both.
- **One box of free text in the header, with addresses made into
  links.** Simpler to explain. Length and the number of links are harder
  to bound, and the layout at phone width is no longer predictable.
- **An end image in place of the happy owl.** The footer now carries the
  organisation to the end of every page, and the thanks picture is a
  choice of its own, with the owl kept as the default.
- **A setting that hides Earful's mark.** A checkbox to explain, store,
  translate and test. Hiding the owl when the survey has a logo gives
  the same page with nothing to set.
- **A `version_brands` table, one row per version.** The thank you page
  is frozen as columns on `survey_versions`, under the trigger already
  there. A second table would need its own trigger, its own purge step
  and a join on every respondent page, for a document that is always
  read whole.
- **Refusing an image over a pixel limit.** Needs only the standard
  library. A photograph from a phone is wider than any limit worth
  serving, so the creator would be sent away to resize it; scaling on
  the server accepts it and stores less.
- **A style kept on the survey, outside the version,** as the title and
  the close date are. No republish to change a logo. A respondent half
  way through a paged survey could see the page change, the preview
  could not show a style before it went live, and an export could not
  say what any respondent saw.
- **A Cloud Storage bucket for images.** Unbounded and cheap per byte.
  It adds a bucket, IAM, a lifecycle rule and a second retention story,
  and the images would still be proxied through the application to stay
  first party.
- **Images linked by address.** Nothing to store. A third party request
  from every respondent's browser, which ADR-0006 exists to prevent.
- **Images as `data:` URLs in the page.** The policy allows them and no
  route is needed. Every page view carries the whole image and it cannot
  be cached.
- **Images owned by the workspace.** Reuse across surveys without a
  copy. Purge then needs a reference count across surveys and drafts,
  and a workspace's own style belongs with Survey Defaults (ADR-0019),
  which can copy a style into a survey when one is made.
- **Checking logos or links automatically** against known marks or a
  reputation service. Heavy, wrong often enough to need a person anyway,
  and a request to a third party about a creator's content. A report
  link and a prompt takedown cover the same risk.

## Consequences

- ADR-0016 is amended: its theme is now the display mode, the
  respondent's page may take one of four themes, and the owl in the
  respondent's footer gives way to a survey's own logo. The style guide
  gains the themes, their measured pairs in both display modes, the
  header, the creator's footer, the plate, the three illustrations, and
  the rule that nothing readable is drawn on a banner.
- The gallery grows. A theme sheet, a development only page that draws
  every themed component once, is pictured in every theme and display
  mode, so each pair is seen and scanned even where a component appears
  on one real page. Real pages are pictured in every theme where colour
  and layout meet, and sampled elsewhere.
- `golang.org/x/image` joins `go.mod`, for scaling and for decoding
  WebP.
- The workspace export moves to its next format version: each version
  carries a `style` object, and the archive gains an `images/` folder
  named by hash. Images count toward the archive's size cap. Existing
  fields are unchanged.
- The purge job deletes `survey_images` with the survey. An image
  referenced by no version and not by the current draft is removed after
  seven days, so replaced uploads do not accumulate.
- Images are in the database, so they are in the daily dumps for as long
  as ADR-0008 keeps them. Erasure of an image is complete when the last
  dump holding it expires, as it is for every other row.
- A change to a tagline is a new version, in the version list and the
  Audit Log, as a change to the thank you message is.
- The re-encoding step is written so that pictures in questions
  (ADR-0021) can share it.
- No new processor, no new infrastructure, no change to the policy, and
  nothing a self hoster must configure beyond the contact address that
  the report link uses.
- The anonymity of a respondent is unchanged: the image route keeps no
  record of who fetched what, and an image request is not an open.
