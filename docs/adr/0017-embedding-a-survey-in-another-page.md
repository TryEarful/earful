# A survey is embedded by a link first, and framed only where its creator names the site

Status: Proposed

A creator can put a survey on a page of their own in two ways. The
first is a snippet of HTML that is a link styled as a button and points
at the survey's ordinary address. It works in a web page and in an
email, changes no header and needs no setting. The second is an
`<iframe>`, offered only for an open anonymous survey whose creator has
listed the sites allowed to frame it. Framing is served from a route of
its own, `/e/{surveyID}`, whose Content Security Policy names those
sites in `frame-ancestors`. Every other address, `/s/{surveyID}`
included, keeps `frame-ancestors 'none'` and `X-Frame-Options: DENY`,
and so does `/e/` for a survey that lists no site.

## Context

Security headers were set in M4-T7 (`internal/http/security.go`) with
one policy for every response, first party only, with
`frame-ancestors 'none'`. The comment there and the backlog line in
`SPEC.md` and `PLAN.md` hold embedding back "pending a CSP/anti-bot
redesign". Reading the respondent path against an iframe shows how much
of that redesign is needed:

- **The anti-abuse checks carry no cookie.** The form token
  (`antibot.FormTokens`) is a signed hidden field, the double submit
  nonce is a hidden field, the ALTCHA challenge is fetched from
  `/s/{surveyID}/challenge` and returned in the form, and the rate
  limits key on the client address (ADR-0006, M4-T5). None of it
  depends on a cookie reaching a third party frame, so none of it
  changes.
- **A form inside the frame posts as same origin.** The frame's
  document is Earful's, so its submit carries `Sec-Fetch-Site:
  same-origin` and passes the stdlib `CrossOriginProtection` wall in
  `NewHandler`. A page around the frame that posts to Earful itself is
  cross site and is still refused. The voice socket accepts only an
  `Origin` equal to its host (`internal/ws`), which a framed Earful
  page satisfies and the host page does not.
- **Cookies do not survive the frame.** Every cookie Earful sets is
  `SameSite=Lax`, which a browser does not send on a request from a
  frame whose top level page is another site, and browsers increasingly
  refuse third party cookies outright. The only cookie on the
  respondent path is the "you already answered" note
  (`setAnsweredCookie`), a courtesy (story 43). The interface language
  falls back from its cookie to `Accept-Language`; the survey's language
  lives in the address (`applyLanguage`), which survives.
- **What really changes is the header, and who may set it.** Allowing
  any site to frame a survey is what makes clickjacking possible: a
  page that lays the frame under its own controls can lead a person to
  submit answers they did not mean to, or to press the dictation button
  and accept the browser's microphone prompt. It can also wrap the
  survey in words that contradict the anonymity notice inside it
  (ADR-0003). Limiting framing to sites the creator names puts that
  risk with the one party who already decides what the survey says.

An email client renders no iframe and runs no script, so "embed in an
email" can only ever be a link. A web page can take either.

## Decisions

**A link and button snippet comes first, and needs no decision about
headers.** The Sharing card shows a short block of HTML: an `<a>` to the
survey's share address with inline styles in the brand's Ink and Paper,
so it looks the same pasted into a page or an email. Inline styles in
the creator's page or email are outside Earful's CSP, which governs only
pages Earful serves. Nothing about the respondent path changes, and the
risk is that of the share link, which already exists.

**Framing is a separate route, not a header switched on `/s/`.**
`/e/{surveyID}` renders the same respondent page through the same
handlers, with three differences: its CSP names the survey's sites in
`frame-ancestors`, its form posts back to `/e/`, and its footer links
open in a new tab, since Help and the privacy page keep `frame-ancestors
'none'` and would otherwise draw nothing inside the frame. Keeping the
share link unframeable means a creator who embeds nothing changes
nothing, and a header test on `/s/` keeps meaning what it says.

**Sites are listed, never `*`.** A creator names each site as an origin
(`https://example.org`). It is parsed, lower cased and written back by
the server; a path, a wildcard, a quote, a semicolon or a space is
refused, so a stored value cannot add a directive to the header.
Outside development an origin must be `https`. A survey lists a small
number of sites, and the setting belongs to the survey like its Close
Date, outside the versioned structure.

**`X-Frame-Options: DENY` stays on every response, `/e/` included.**
A browser that understands `frame-ancestors` ignores `X-Frame-Options`
when both are present; one too old to understand it refuses to frame.
The failure is closed: the oldest browsers show an empty frame, and the
frame snippet carries the survey's ordinary link beneath it, which every
browser can follow.

**Only open anonymous surveys are framed.** An invited survey is
answered through a personal address whose token is personal data; it
does not belong in another site's page source. `/e/` for an invited,
closed or draft survey renders the same refusal as `/s/`, under
`frame-ancestors 'none'` unless the survey lists sites.

**The frame sets no cookie.** The answered note is not shown inside a
frame and not written from one. A partitioned cookie could carry it,
and is listed as a later step; the note is a courtesy and an
anonymous survey accepts repeat answers by design.

**The snippet asks the host page to send nothing about itself.** It
carries `referrerpolicy="no-referrer"`, so Earful is not told which
page a respondent was reading, and `loading="lazy"`, so a frame far
down a page is opened only when it is scrolled to. It carries a `title`
for screen readers and, where dictation is offered, `allow="microphone"`,
without which a browser refuses the microphone inside a frame. The
host page loads no script from Earful.

**An embedded page is counted as an open, like any other.** Opened,
submitted and where answers stop are recorded as on `/s/` (ADR-0012).
No metric is added and nothing about the frame is recorded with a
response.

## Considered Options

- **Allow framing everywhere, `frame-ancestors *`, for every survey.**
  One line in `security.go`, and every published survey becomes a
  clickjacking target on any site, including the dictation consent.
  Rejected.
- **Per survey opt in with `*`.** The creator chooses, and once chosen
  anyone may frame the survey, including a site that wants its answers
  wrong. An allowlist costs the creator one line per site. Rejected;
  reopening this ADR is the way to add it if allowlists prove too much.
- **Relax the header on `/s/` for surveys that opt in.** No new route,
  but the share link and the embedded page become one address with two
  policies, and the existing header test on `/s/` stops proving that
  sharing a link never makes a survey frameable.
- **A script the host page loads, which draws the survey into the
  page.** It could size itself and style itself to fit, and it is a
  script from Earful running with the host page's authority, with the
  answers in a document the host page can read. That breaks the
  separation the frame provides and turns ADR-0006 inside out. Rejected.
- **A frame that resizes itself by `postMessage`.** Needs a script on
  the host page to listen. Left for later and optional: a fixed height
  suits a form shown one question at a time.
- **`SameSite=None` cookies on the respondent path.** Restores the
  answered note in some browsers and not in others, and makes a cookie
  that was first party into a third party one. Not done.
- **Answer the first question in an email**, with each choice a link
  that records it. A `GET` that writes is reached by every link scanner
  that opens mail, which the magic link flow already has to defend
  against. A choice link that only fills in the answer on the survey
  page is safe and is a separate story.

## Consequences

- `security.go` gains a way for one handler to name its frame ancestors;
  the default stays `'none'`, and the comment that lists embedding as
  out of scope is replaced by a pointer here.
- A migration adds the list of sites, removed with its survey by the
  purge job, which deletes children explicitly rather than by cascade.
- The workspace export carries each survey's sites and moves to format
  version 3, documented in `docs/export-format.md`, unless the owner
  decides they are instance configuration and leaves them out.
- A respondent reading an embedded survey is on a page Earful does not
  control. What Earful stores is unchanged, and the anonymity notice in
  the frame stays true of Earful. The host page can know that its
  visitor saw the survey, as it knows everything else on its page; the
  trust page says so.
- Opened counts rise for a survey embedded on a busy page, since a
  visit that scrolls past the frame is an open. Lazy loading narrows
  this; the stats page notes it for a survey that lists sites.
- Amends ADR-0006: respondent pages still load nothing from a third
  party, and an embedded one is itself loaded by one. Amends ADR-0016:
  the respondent footer opens its links in a new tab when framed.
  Amends the M4-T7 header decision recorded in `PLAN.md`.
