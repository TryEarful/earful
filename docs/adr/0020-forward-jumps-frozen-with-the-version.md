# Survey logic is forward jumps on a question's own answer, frozen with the version

Status: Proposed. Would amend ADR-0001 (what a version holds) and
ADR-0012 (how "where answers stop" reads on a branched survey).

A question may carry logic: an ordered list of jumps, each a condition
on that question's own answer and a destination. The destination is a
later question, named by its Question Identity, or the end of the
survey. After a respondent answers a question, the first jump whose
condition holds decides what comes next; when none holds, the next
question by position does. A jump never points backwards, so every path
through a survey is a subsequence of its questions in position order,
and a survey with no logic behaves exactly as it does today.

| Question type | Conditions |
|---|---|
| `single_choice`, `dropdown` | the answer is one of a set of options |
| `multiple_choice` | the answer includes any of a set of options |
| `yes_no` | the answer is yes, or is no |
| `rating_scale`, `nps` | the answer is at most, at least, or between two points |
| every type | the question was left unanswered |

Logic is written in the draft, as a `logic` list on each question of the
draft document, and frozen at publish into a `logic` column on the
`questions` row it belongs to, under the trigger that already makes
those rows immutable. An option is named by its text in the creator's
language, which is what an answer already stores.

The server decides the path. Without JavaScript, a survey with logic is
served as pages that each end at a question carrying jumps; "Continue"
posts what has been answered so far, writes nothing, and is answered
with the next page, earlier answers riding along as hidden fields. With
JavaScript, the same rules travel in the page's JSON block and
`respond.js` pages through them one question at a time, as it does now.
Either way the final submission is the only one that counts: the server
works out the path from the canonical answers, validates the questions
on it, and drops any answer to a question that is not on it before
anything is stored.

## Why

A path is then a pure function of two things that are already kept
forever: the version's frozen questions and the response's answers.
Whether a question was shown to a response can be worked out again at
any time, by results, by the export, by an importer, without a column
that records a respondent's route. ADR-0003 and ADR-0009 are kept
without a word changed, because nothing new is stored about anyone.

Forward only makes a cycle impossible to write rather than something to
detect, keeps position a valid order for every path, and leaves
ADR-0012's counters keyed by identity meaning what they meant. Conditions
on the question's own answer only make reachability exact: since each
answer is chosen freely, a question no path reaches can be found by
walking a graph, and the editor can say so.

Jumps are what a creator draws. The issue asks to link questions to
each other, and a link from an answer to a question is a jump; a flow
diagram is a picture of the jumps and nothing else.

## Considered Options

- **Show if conditions on each question**, over any earlier answers,
  combined with and and or. More expressive, and order independent. But
  there is no arrow to draw: the diagram the issue asks for would be a
  rendering of boolean expressions. Ending a survey early becomes a
  condition repeated on every question after the branch. Checking that a
  question can be reached is satisfiability rather than a graph walk.
  A later amendment can add conditions on earlier answers to a jump
  without changing where logic is stored.
- **Jumps to any question, with cycle detection at publish.** A loop is
  never what a survey means, a backward jump makes "where answers stop"
  ambiguous, and the validator would be the only thing standing between
  a draft and a respondent who never reaches Send.
- **Pages or sections as the unit of logic**, as some form builders do.
  Earful has no sections: a respondent sees one question at a time. A
  section would be a new concept in the schema, the editor and the
  export to serve an internal grouping that the server can work out.
- **Logic in a table of its own** (`question_jumps`). Relational, and
  queryable, but logic is never queried: it is read whole with the
  version, as options are. A `jsonb` column beside `options` needs no
  new trigger, no new purge step and no new join.
- **Options named by a stable id** rather than by text. A rename would
  not break a jump. But answers, exports, localizations and results all
  name an option by its text, and introducing ids for jumps alone would
  leave two ways to name one thing. Within a version text is as stable
  as an id, since the version cannot change. In the draft, a jump whose
  option has gone is kept, marked and refused at publish.
- **Evaluating only in the browser**, with every question in one long
  form for a browser without scripts. Breaks the rule that every feature
  works without JavaScript, and the server would store whatever a
  modified page sent, including answers to questions that were never on
  the respondent's path.
- **A progress beacon per step**, so the server holds the path in a
  session. The first time a respondent page would report behaviour
  before submitting, which M7-T4 and ADR-0012 have declined twice, and
  state held about an anonymous respondent between requests.
- **Hiding questions with CSS `:has()`** keyed on checked inputs. Works
  without scripts in recent browsers, but the selectors depend on each
  survey, and the Content Security Policy allows styles only from the
  site's own stylesheet (`style-src 'self'` in
  `internal/http/security.go`).

## Consequences

- A Survey Version is its questions, its Localizations and its logic.
  Adding, changing or removing a jump is a change that publishes a new
  version; the comparison that refuses an unchanged republish compares
  logic too. Migration adds `questions.logic jsonb NOT NULL DEFAULT
  '[]'`, so every existing version reads as linear; adding a column with
  a constant default rewrites no row and fires no update trigger.
- "Required" means required when shown. A question off a respondent's
  path is never asked for.
- A draft change that breaks a jump (an option renamed or removed, a
  type changed, a target deleted or moved above its source) keeps the
  jump, marks it in the editor in words, leaves it out of preview, and
  refuses publish until it is fixed or removed. Nothing a creator wrote
  is dropped without them seeing it.
- Localized options are positional, as they already are. The server
  maps a localized answer to the creator's wording before evaluating;
  the script is given each condition as option positions, since what it
  sees on the page is the localized text.
- A survey without logic is served as it is today, one page without
  JavaScript and one question at a time with it. The paged form exists
  only for surveys that need it. "Continue" is a POST that writes
  nothing and counts nothing: it is not a start (ADR-0012), it is rate
  limited per network and survey, and it carries the signed form token
  from the first page, so the minimum fill time on the final submit is
  measured from when the survey was first shown.
- With JavaScript, the questions after the first page arrive inside a
  `<template>` element, which a browser without scripts neither shows
  nor submits. No new request, no inline script, no change to the CSP.
- Results count a question's skips among the responses it was shown to,
  not among all responses, and say how many it was shown to. The CSV
  leaves a cell empty both for a skip and for a question not shown; the
  JSON export carries the logic, so the difference can always be worked
  out.
- The workspace export moves to its next format version: each question in
  `versions[].questions[]` carries `logic`. `docs/export-format.md` says
  how an importer recomputes a path, and that it must keep identities
  for jumps to mean anything.
- ADR-0012's "where answers stop" counter is unchanged. On a branched
  survey a submitted response can stop at the end of any branch, so the
  stats page marks questions that end a branch rather than presenting
  every stop as a drop. No per path counter is added: the blessed list
  in ADR-0009 stays exhaustive.
- Insights tell the model how many responses each question was asked
  of. Generation may propose jumps among the questions of one run, held
  to the same validation, on the same EU endpoint (ADR-0011, ADR-0013);
  a jump that fails validation is dropped and counted in the notice.
- Purge and erasure need nothing new. Logic lives in the draft document
  and in `questions` rows, and goes with them.
- The editor works with forms first: each question card lists its jumps
  in words and adds one with a condition and a destination, chosen from
  lists the server draws for that question's type. The flow diagram and
  drag and drop come after, as enhancements over those same forms, which
  stay on the page as the keyboard and single pointer alternative to
  dragging. A connector, a jump row and the diagram are components the
  style guide gains in the same change (ADR-0016).
