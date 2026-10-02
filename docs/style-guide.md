# Style guide

How Earful looks and how it speaks. The values live in
`web/static/css/app.css`, at the top, as custom properties; this page
says what each one is for. `make gallery` shows every page at a phone's
width and a desktop's, in light mode and dark, in English and
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
| `--sand` | `#D2B98C` | The owl's wings and eye rings |
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

Components use only these, and dark mode and the themes reassign only
these:
`--bg`, `--surface`, `--surface-2`, `--text`, `--muted`, `--link`,
`--focus`, `--accent`, `--accent-contrast`, `--border`, `--border-strong`,
`--danger`, `--button`, `--button-text`, `--ring`, the `--tint-*` tokens
and the `--owl-*` tokens.

### Dark mode

Light and dark are the two display modes.

Ink ground, a slightly lighter card, Paper type. The filled button turns
Paper with Ink type. Links and focus become a lighter teal, the owl's
body lifts to `#2A3A4C` with a Feather outline so it does not vanish,
and chart 1 and 3 are lifted until they hold 3:1 on the dark card.

A page follows the reader's system until they choose otherwise with
the display mode switcher in the footer of every page, a survey's
included: follow system, light or dark. The choice is kept in a `mode`
cookie and the server writes it as `data-mode` on `<html>`, so the page
arrives in its mode with no flash and no script. The stylesheet draws
the dark tokens in two places: under `prefers-color-scheme: dark` where
`data-mode` is not `light`, and under `data-mode="dark"`. The two
blocks hold the same values, and a test fails the build if they drift
apart, so a change to a dark token is made in both. `color-scheme` and
the `theme-color` tags follow a chosen mode too, and the owl, coloured
by the `--owl-*` tokens, follows with them. The favicon is a file in
fixed colours and does not.

### Themes

A survey's creator can have its pages drawn in a theme (ADR-0018):
Earful, which is everything above and the default, or Slate, Ocean or
Forest. A theme is for a respondent's pages only. The creator's own
pages, Help and the documents are always Earful.

| Theme | Light | Dark | Button and links |
|---|---|---|---|
| Earful | Paper page, white card | Ink page | Ink button, teal links |
| Slate | Cool grey page, white card | Neutral near black | Charcoal button, grey blue links |
| Ocean | Pale blue page, white card | Deep navy | Blue button and links |
| Forest | Pale green page, white card | Deep green black | Green button and links |

Slate is the neutral one: it has no colour of its own, so whatever a
survey brings is the only colour on the page.

A theme is a list of tokens and nothing else. It sets `--bg`,
`--surface`, `--surface-2`, `--text`, `--muted`, `--link`, `--focus`,
`--accent`, `--accent-contrast`, `--border`, `--border-strong`,
`--button`, `--button-text`, `--ring` and `--tint-info`, in a light
block and a dark one, and no component rule knows a theme exists. A
respondent's display mode chooses between the two blocks, as it does for
Earful: a theme never decides light or dark for the reader.

A theme may not set anything else:

- **Signal stays Signal.** Coral means a microphone is open in every
  theme, and no theme has a colour near it.
- **Status stays status.** `--good`, `--warning`, `--danger` and their
  tints are the same in every theme, so an error looks like an error on
  every survey. This is why a theme's grounds are tints close to white
  and close to black and never a saturated colour: the status colours
  have to read on them.
- **Type, space, shape and motion** are the same in every theme.

The class is `theme-slate`, `theme-ocean` or `theme-forest` on `<html>`,
written by the server from the style the survey's version was published
with; Earful has no class. The same class on any element draws what is
inside it in the theme, which is how the Style tab shows a sample of
each. A new theme is added here first, with its row in the table below.

### Contrast

Measured, WCAG 2.1, for every theme in both display modes. The table is
written from the stylesheet by `web/static/contrast_test.go`, which
fails the build when a pair falls below its ratio: 4.5:1 for text, 3:1
for a mark that carries meaning. Signal on the light page is below 3:1
in Earful, where the record button's shape and its label carry the
meaning with it; no theme may be below Earful there. After changing a
token, rewrite the table with
`go test ./web/static -run Contrast -update-contrast`.

The axe scan in the e2e suite and in the gallery checks every page in
both modes, and the gallery also pictures a few pages with a mode chosen
against the system's, dark on a light system and light on a dark one.

<!-- contrast table: written by web/static/contrast_test.go -->
| Pair | Earful light | Earful dark | Slate light | Slate dark | Ocean light | Ocean dark | Forest light | Forest dark |
|---|---|---|---|---|---|---|---|---|
| Text on page | 15.99 | 15.99 | 16.02 | 16.60 | 15.61 | 16.39 | 15.17 | 16.49 |
| Text on card | 17.84 | 13.86 | 17.82 | 14.70 | 17.28 | 14.12 | 16.81 | 14.39 |
| Text on a field's ground | 16.83 | 14.89 | 16.77 | 15.83 | 16.34 | 15.31 | 15.94 | 15.50 |
| Muted on page | 8.13 | 10.70 | 7.14 | 10.73 | 7.88 | 10.49 | 7.84 | 10.75 |
| Muted on card | 9.08 | 9.27 | 7.94 | 9.50 | 8.73 | 9.03 | 8.68 | 9.38 |
| Muted on a field's ground | 8.56 | 9.96 | 7.47 | 10.23 | 8.25 | 9.79 | 8.23 | 10.10 |
| Link on page | 7.12 | 9.59 | 7.84 | 9.62 | 6.41 | 9.06 | 6.68 | 10.15 |
| Link on card | 7.95 | 8.31 | 8.72 | 8.52 | 7.10 | 7.81 | 7.40 | 8.85 |
| Link on a field's ground | 7.50 | 8.93 | 8.21 | 9.18 | 6.71 | 8.47 | 7.01 | 9.54 |
| Text on a notice, on page | 16.15 | 12.29 | 15.70 | 12.44 | 15.04 | 12.39 | 14.76 | 12.10 |
| Text on a chosen option | 15.99 | 12.01 | 15.96 | 12.81 | 15.54 | 12.31 | 15.13 | 12.44 |
| Filled button's text on it | 15.99 | 15.99 | 13.03 | 16.02 | 8.21 | 10.58 | 8.16 | 11.74 |
| Text on the accent | 7.95 | 9.59 | 8.72 | 9.62 | 7.10 | 9.06 | 7.40 | 10.15 |
| Text on a delete button | 6.32 | 7.47 | 6.32 | 7.73 | 6.32 | 7.60 | 6.32 | 7.66 |
| Danger on page | 5.67 | 7.47 | 5.68 | 7.73 | 5.71 | 7.60 | 5.71 | 7.66 |
| Danger on card | 6.32 | 6.47 | 6.32 | 6.84 | 6.32 | 6.54 | 6.32 | 6.68 |
| Good on its tint, on card | 5.58 | 6.26 | 5.58 | 6.59 | 5.58 | 6.33 | 5.58 | 6.40 |
| Danger on its tint, on card | 5.22 | 5.18 | 5.22 | 5.34 | 5.22 | 5.28 | 5.22 | 5.33 |
| Warning on its tint, on card | 4.82 | 6.12 | 4.82 | 6.34 | 4.82 | 6.25 | 4.82 | 6.24 |
| Ink on Signal | 5.54 | 5.54 | 5.54 | 5.54 | 5.54 | 5.54 | 5.54 | 5.54 |
| Ink on Signal's soft tint | 14.69 | 14.69 | 14.69 | 14.69 | 14.69 | 14.69 | 14.69 | 14.69 |
| Focus ring on page (3:1) | 7.12 | 9.59 | 7.84 | 9.62 | 6.41 | 9.06 | 6.68 | 10.15 |
| Focus ring on card (3:1) | 7.95 | 8.31 | 8.72 | 8.52 | 7.10 | 7.81 | 7.40 | 8.85 |
| Accent on card (3:1) | 7.95 | 8.31 | 8.72 | 8.52 | 7.10 | 7.81 | 7.40 | 8.85 |
| Filled button on page (3:1) | 15.99 | 15.99 | 12.67 | 16.60 | 7.42 | 10.58 | 7.37 | 11.74 |
| Signal on card (3:1) | 3.22 | 4.80 | 3.22 | 5.07 | 3.22 | 4.85 | 3.22 | 4.95 |
| Signal on page | 2.89 | 5.54 | 2.90 | 5.73 | 2.91 | 5.63 | 2.91 | 5.68 |
<!-- end of the contrast table -->

Chart 1 on the card holds 5.38 in light mode and 5.54 in dark. Charts
are on the creator's pages, which have no theme.

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
| `.checkbox` | A single tickable row, at least 44px tall |
| `input[type="file"]` | A file picker inside a `.field`: its button is the outlined pill, with a hint below saying what it takes |
| `.facts` | Labels and their values; one under the other on a page that stands alone, such as the answers read back after sending |
| `.tabs` | The views of one survey |
| `.responses` inside `.table-scroll` | A table, which scrolls sideways on its own |
| `.empty-state` | The owl, a line and what to do |
| `.site-header`, `.site-footer`, `.respond-footer` | The chrome |
| `.switchers`, `.switcher` | The footer's quiet choices: the language and the display mode |
| `.theme-choices`, `.theme-choice` | The themes offered on the Style tab: a `.choice` for each, with its sample |
| `.theme-sample` | A small page in a theme's own tokens: its ground, a card, a line of text, a link and its filled button, which is not a control |
| `.focus-shown` | The focus ring, drawn on the theme sheet's samples where a picture cannot hold the keyboard's focus. Nowhere else |

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
display mode. It is decoration: whatever stands beside it says what it means.

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
phone's width (390px) and a desktop's (1280px), in light mode and
dark, in English and Spanish. `make gallery` takes those pictures and
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
8. **Accessible in both display modes.** Dark mode is as finished as
   light, axe reports nothing, and focus is visible.

A respondent's page is also reviewed in each theme, against four more:

9. **Signal alone is coral,** and it stands out from the theme's grounds.
10. **Focus shows** on the page, on a card and on a field.
11. **Status reads as status:** an error is red and a notice is not.
12. **The filled button is the main action,** and nothing else on the
    page looks like one.

A page is done when it passes every criterion in two reviews in a row,
the second made after the fixes from the first.
