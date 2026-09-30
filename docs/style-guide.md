# Style guide

How Earful looks and how it speaks. The values live in
`web/static/css/app.css`, at the top, as custom properties; this page
says what each one is for. `make gallery` shows every page at a phone's
width and a desktop's, in the light theme and the dark, in English and
Spanish, and is how a change to any of it is looked at before it is
committed.

## Principles

- **Calm and direct.** One idea per sentence, one filled button per
  screen, nothing on a page that does not help somebody do what they came
  to do.
- **Warm, not cute.** Paper, not white; rounded, not bubbly; the owl in a
  few chosen moments, never beside data.
- **Voice is the only loud thing.** Signal coral is reserved for the
  moment a microphone is open.

## Colour

### Palette

| Token | Value | Role |
|---|---|---|
| `--ink` | `#101823` | Text, the filled button, the owl's body |
| `--ink-2` | `#22303F` | Headings on tinted ground |
| `--slate` | `#3A4A5C` | Secondary text |
| `--deep-teal` | `#0E5A5E` | Links, focus rings, a selected choice |
| `--teal` | `#177A77` | Interactive tint |
| `--feather` | `#EAD9B4` | Warm fills; the owl's outline in the dark |
| `--sand` | `#D2B98C` | The owl's ear tufts, wings and eye rings |
| `--paper` | `#F7F2E8` | The page. A page is never plain white |
| `--white` | `#FFFFFF` | Cards and fields on the page |

### Signal

`--signal` `#FF5238`, `--signal-ink` `#C53621`, `--signal-soft` `#FFE3DD`.

Signal means "the microphone is open". It is the record button while it
records, the field the words are landing in, the sound waves beside the
listening owl, and the owl's beak. It is never a call to action, a
badge or decoration. Text on Signal is Ink: white on Signal reads only
at large sizes.

### Voice colours

Each survey has one of ten Voice colours (`--voice-1` to `--voice-10`),
worked out from its ID (`SurveyView.VoiceIndex`), so it never changes and
needs nothing stored. It is a stripe down the survey's card on the
dashboard: a way to tell surveys apart, with no meaning beyond that. Two
surveys may share one.

Charts use `--chart-1` to `--chart-6`, which are Voice 1 to 6, in that
order, never cycled. One measure is one hue: submissions are chart 1,
opens are chart 2. Labels and counts are Ink, beside the bar, never on
it.

### Status

`--good`, `--warning`, `--serious`, `--critical`, and a tint of each for
the ground of a chip. Status colours are never a chart series.

### Semantic tokens

Components use only these, and the dark theme reassigns only these:
`--bg`, `--surface`, `--surface-2`, `--text`, `--muted`, `--link`,
`--focus`, `--accent`, `--accent-contrast`, `--border`, `--border-strong`,
`--danger`, `--button`, `--button-text`, `--ring`, the `--tint-*` tokens
and the `--owl-*` tokens.

### Dark theme

Ink ground, a slightly lighter card, Paper type. The filled button turns
Paper with Ink type. Links and focus become a lighter teal, the owl's
body lifts to `#2A3A4C` with a Feather outline so it does not vanish,
and chart 1 and 3 are lifted until they hold 3:1 on the dark card.

### Contrast

Measured, WCAG 2.1. The axe scan in the e2e suite and in the gallery
checks every page in both themes.

| Pair | Light | Dark |
|---|---|---|
| Text on page | 15.99 | 15.99 |
| Text on card | 17.84 | 13.86 |
| Muted on page | 8.13 | 10.70 |
| Link on page | 7.12 | 9.59 |
| Good on its tint | 5.58 | 6.23 |
| Danger on its tint | 5.22 | 5.15 |
| Ink on Signal | 5.54 | 5.54 |
| Chart 1 on card (graphics, 3:1) | 5.38 | 5.54 |

## Type

Manrope, served from `web/static/fonts` under the SIL Open Font
License, as one variable file for Latin and one for extended Latin that
loads only where a page needs it. Stylesheets and fonts may only come
from Earful's own origin (ADR-0006), so nothing is fetched from a font
service. Avenir Next and the system face stand in while it loads.

| Use | Size | Weight | Tracking |
|---|---|---|---|
| Hero | 40 to 64px | 800 | -0.03em |
| Page heading (h1) | 28 to 38px | 800 | -0.025em |
| Section heading (h2) | 20px | 800 | -0.015em |
| Question | 22px | 800 | -0.015em |
| Sub heading (h3) | 17px | 700 | -0.01em |
| Body | 16px | 500 | 0 |
| Labels, small | 14px | 600 to 700 | 0 |
| Table headers, figures' labels | 11px | 800 | 0.08em, upper case |

Headings are in sentence case. Upper case is set by the stylesheet, and
only for table headers and the labels over figures. Counts, dates and
times use tabular figures.

## Space, shape, depth, motion

- Space: 4, 8, 12, 16, 24, 32, 48, 64 (`--space-1` to `--space-8`).
- Radius: 8 for fields and chips, 14 for cards, 22 for dialogs. Buttons
  are always pills.
- Depth: a card has a hairline border and a soft shadow; a card being
  pointed at lifts two pixels onto a deeper one.
- Motion: `--ease` over 160ms for hovers, 260ms for cards, 420ms for the
  owl. The recording pulse is one calm breath of 1.6s. Everything that
  moves stands still for a reader who asked for reduced motion.

## Components

| Class | What it is |
|---|---|
| `button`, `.button` | The filled pill: the page's one main action |
| `.secondary` | The outlined pill: every other action |
| `.secondary.danger` | Deleting something |
| `.button-link` | An action set as a link |
| `.card` | A white panel; a page is a stack of them |
| `.chip`, `.chip-open`, `.chip-draft`, `.chip-closed` | Status, with a dot that repeats the word |
| `.notice`, `.error-summary` | A message about what just happened |
| `.field`, `.choice`, `.option`, `.scale-point` | Form rows |
| `.tabs` | The views of one survey |
| `.responses` inside `.table-scroll` | A table, which scrolls sideways on its own |
| `.empty-state` | The owl, a line and what to do |
| `.site-header`, `.site-footer`, `.respond-footer` | The chrome |

Every link and control is at least 44px tall. Focus is a 3px teal ring,
two pixels out.

### Hooks are not styles

A class that starts with `js-` is how a script or a test finds an element
and is never styled; a test fails the build if a stylesheet names one. A
state a script sets is an `is-` class or a `data-` attribute. See
CONTRIBUTING.md.

## The owl

A round owl with ear tufts, drawn inline by `owl` in
`web/templates/brand.templ`, so that the stylesheet colours it in either
theme. It is decoration: whatever stands beside it says what it means.

| Mood | Where |
|---|---|
| Neutral | The wordmark, the favicon, an empty dashboard |
| Happy (eyes closed in a smile) | Answers sent; a preview submitted |
| Surprised (a small "o") | A link that leads nowhere, a sign in link that cannot be used |
| Listening (sound waves) | The dictation card; the waves show only while recording |

Never beside results, stats, charts, admin tools, the trust and privacy
pages, or a failure that costs somebody something.

The wordmark is "earful" in lower case, heavy and tight, with the owl to
its left. In prose the name is Earful.

## Words

- Short sentences. Contractions. Say what happens, not how clever it is.
- No dashes of any kind in anything a reader reads: join clauses with a
  full stop, a comma, a colon or the middle dot, and write compounds as
  separate words ("sign in", "open source"). A hyphen is kept only where
  it is part of a code, an address or a language tag. Tests read every
  message in every language, every template and every document, and fail
  on a dash (`internal/uitext/dash_test.go`, `internal/pages/dash_test.go`).
- Sentence case everywhere. Page titles read "Surveys · Earful".
- "All ears" is the one idiom, for loading and empty moments.
- Spanish addresses the reader as usted and should read as if written in
  Spanish. The tagline "Surveys, out loud." stays in English.

## Reviewing a page

Every page a change adds or alters is reviewed against this list, at a
phone's width (390px) and a desktop's (1280px), in the light theme and
the dark, in English and Spanish. `make gallery` takes those pictures and
an axe report for each; a new page is added to `e2e/gallery/gallery.spec.ts`
so that it is pictured too. Each criterion passes or fails, with a reason.

1. **Polished.** Spacing on the scale above, radii and type from the
   tokens, nothing cramped, clipped or left alone on a line.
2. **Clear hierarchy.** One filled button, for the page's main action;
   headings on the type scale; the next step is the most visible thing.
3. **Intuitive.** Someone new knows what to do without the Help page.
   Labels are short and say what happens.
4. **Clean.** No box inside a box, no sentence said twice, no card that
   holds a single line.
5. **On brand.** Colours by their roles, Signal only where a microphone is
   open, the owl only where its moods are listed and never beside data.
6. **Words.** Sentence case, short sentences, no dashes, Spanish that
   reads as written in Spanish. Wording lives in `web/text`, never in a
   template.
7. **Responsive.** Nothing scrolls sideways at 390px; every link and
   control is at least 44px tall.
8. **Accessible in both themes.** The dark theme is as finished as the
   light one, axe reports nothing, and focus is visible.

A page is done when it passes every criterion in two reviews in a row,
the second made after the fixes from the first.
