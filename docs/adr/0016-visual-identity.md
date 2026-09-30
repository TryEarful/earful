# One visual identity, drawn under the page's own rules

Every page of Earful is drawn from one set of tokens at the top of
`web/static/css/app.css`: a Paper ground, Ink type and buttons, white
cards, Deep Teal for links and focus, ten Voice colours for surveys and
charts, and Signal coral only where a microphone is open. A dark theme
reassigns the same semantic tokens on an Ink ground. The mark is a round
owl, drawn inline, in four moods. `docs/style-guide.md` describes the
system; this records the decisions that shaped it.

## Decisions

**The owl is inline SVG, not an image file.** The Content Security Policy
allows styles only from Earful's own stylesheet (`style-src 'self'`), so
an SVG file cannot carry rules of its own and could not change colour
with the theme. Inline, each part is filled through a class, and one
`prefers-color-scheme` block recolours every owl on the site. The
favicon and the touch icon are the only files, in fixed colours, with a
Feather outline so they hold their edge on a dark tab bar.

**The dark theme is derived, not designed from scratch.** The brand has
no dark theme. Only the semantic tokens change, so no component knows
which theme it is in, and each pair of colours is measured and listed in
the style guide. The axe scan runs on every page in both themes.

**A survey's colour is worked out from its ID.** Hashing the ID onto the
ten Voice colours needs no column, no migration and no setting, and
cannot change. Two surveys may share a colour; since the colour means
nothing beyond "this one", that costs nothing. A stored, chosen colour
can replace it if creators ask to pick one.

**The font is served with the page.** ADR-0006 keeps respondent pages
free of third party requests, so Manrope is embedded under its open
licence in `web/static/fonts`: a variable file for Latin, preloaded, and
one for extended Latin that a browser fetches only for a page that uses
its letters. Go has no type for `.woff2`, so the static package registers
one; without it the files would be refused under `nosniff`.

**Scripts and tests hook onto `js-` classes.** Styling classes can then
be renamed freely, and a restyle cannot break a script or a test. A test
fails the build if a stylesheet styles a `js-` class.

**No dashes in what a reader reads.** Clauses are joined with full
stops, commas, colons and the middle dot; compounds are written as
separate words. Tests read every message, template and document and fail
on a dash outside code, addresses, placeholders and language tags.

**The respondent's page has a footer.** It had none, on purpose: a
respondent has no account and nothing to navigate. It now carries Help
and the privacy page, which a respondent may need before answering, and
nothing else. On the privacy and trust pages the owl is left out of it.

## Considered Options

- **An `<img>` owl with a light and a dark file**, switched with
  `<picture>` and `prefers-color-scheme`. Two files per mood, eight in
  all, and every change drawn twice.
- **Google Fonts for Manrope.** One line, and a request to Google from
  every respondent's browser, which ADR-0006 exists to prevent.
- **A colour column on surveys.** Needed only if a creator is to choose;
  a hash gives the same recognition for nothing.
