# Survey logic and the flow editor (issue #17)

Status: proposed. The decision is ADR-0020; this is the plan for
building it, in slices that each ship on their own.

> As a survey creator, I want to be able to link the different questions
> with a drag and drop functionality easily and visually so that I can
> save time designing the flows of the survey.

What exists today: a draft is a list of questions in order
(`domain.Draft` in `internal/domain/survey.go`), reordered with Up and
Down (`Draft.Move`, posted by `questionMove` in
`internal/http/surveys.go`). Publishing freezes the list into `questions`
rows by position (`publishDraft` in `internal/store/surveys.go`). A
respondent's browser receives every question in one form
(`web/templates/respond.templ`); `web/static/js/respond.js` pages
through it one question at a time, and without JavaScript the form is
one long page. `respondSubmit` and `participantRespondSubmit` in
`internal/http/respond.go` validate every question of the served
version. Nothing anywhere knows about a path.

## The model

A question in the draft gains a `logic` list:

```jsonc
{
  "identity_id": "q3",
  "type": "yes_no",
  "text": "Did you finish setting up?",
  "required": true,
  "logic": [
    { "when": { "op": "is", "values": ["no"] }, "goto": "q7" },
    { "when": { "op": "unanswered" },          "goto": "end" }
  ]
}
```

| `op` | Types | Values |
|---|---|---|
| `is_one_of` | `single_choice`, `dropdown` | option texts, one or more |
| `includes_any` | `multiple_choice` | option texts, one or more |
| `is` | `yes_no` | `yes` or `no` |
| `at_most`, `at_least` | `rating_scale`, `nps` | one point on the scale |
| `between` | `rating_scale`, `nps` | two points, inclusive |
| `unanswered` | every type | none |

`goto` is the identity of a later question in the same draft, or `end`.
Rules are checked in the order listed; the first that holds wins; when
none holds, the next question by position follows. At most 20 jumps per
question.

In Go (`internal/domain/logic.go`, new):

- `type Jump struct { When Condition; Goto string }` and
  `Question.Logic []Jump` with `json:"logic,omitempty"`, so drafts and
  served versions keep sharing one `Question` shape.
- `func Path(questions []Question, answers map[string]AnswerValue) []Question`:
  the questions a respondent with these answers is asked, in order.
  Pure, total and cheap: one pass, never revisits a position.
- `func (d Draft) LogicProblems() []LogicProblem`: every jump that no
  longer fits (unknown or earlier target, an option that is gone, a
  condition the type does not take, a point off the scale), and every
  question no path reaches. `ValidateForPublish` refuses the first kind
  as a `QuestionError`; the second is a warning in the editor only.
- `Submission.Validate` is given the path rather than every question,
  and a new `Submission.OnPath(path)` drops answers to questions off it.

## Slices

Each slice leaves `main` releasable. Tests are at the application edge
(docs/testing.md): HTTP against the real handler and Postgres, with
Playwright for what needs a browser. The evaluator and validator also get
table tests in `internal/domain`, as `survey_test.go` already has for
the draft's other invariants.

### SL-1 Logic in the draft, the editor and preview (L)

The creator can write jumps without JavaScript, see them in words, and
try them in preview. Publishing a draft that has logic is refused with a
plain message until SL-2, so no respondent meets a jump the answering
path does not honour.

- `internal/domain/logic.go`, `logic_test.go`: types, `Path`,
  `LogicProblems`, limits; `Draft.Move`, `Draft.Remove` and
  `Draft.Replace` keep jumps and let `LogicProblems` report what broke.
- `internal/http/surveys.go`: `POST
  /surveys/{surveyID}/questions/{questionID}/logic` (add one jump from a
  condition select, a value control and a destination select) and
  `.../logic/{n}/delete`. Routes in `internal/http/routes.go`.
- `web/templates/surveys.templ`: under each question card, its jumps as
  sentences ("If the answer is No, go to 7. Why not?") with Remove, a
  marked line for each problem, and the add form drawn for that
  question's type. Destinations list only later questions and "End of
  survey".
- Preview (`previewPage`, `previewSubmit`) pages by `Path`, using the
  server paging SL-2 builds for respondents; the slice ships the paging
  code behind preview first.
- `web/text/active.en.toml`, `active.es.toml`: every sentence, in both
  languages, no dashes. `docs/style-guide.md`: the jump row and the
  problem line as components.
- Tests: `internal/http/logic_test.go` adds, lists and removes jumps;
  a renamed option, a deleted target and a move above the source each
  leave a marked jump and refuse publish; a question no path reaches is
  named; preview without JavaScript follows a jump and ends early.
- Gallery: the editor with no logic, with jumps, with a broken jump,
  with an unreachable question; preview on a middle page.

### SL-2 Published and answered (L)

- Migration `db/migrations/000NN_question_logic.sql` (next free number):
  `ALTER TABLE questions ADD COLUMN logic jsonb NOT NULL DEFAULT '[]'`
  with `CHECK (jsonb_typeof(logic) = 'array')`. No backfill: every
  existing version is linear.
- `db/queries/surveys.sql`, `responses.sql`: write and read `logic`;
  `publishDraft` freezes it; `questionsEqual` compares it, so a change
  to logic alone publishes.
- `internal/http/respond.go`: for a version with logic, render the page
  that runs from the current question to the next question carrying
  jumps; answers from earlier pages as hidden `q_<identity>` fields, so
  `parseSubmission` reads the final POST unchanged. `action=continue`
  and `action=back` are answered with the next or previous page after
  the honeypot, a valid form token (age not enforced) and a new
  `limitSteps` limiter; nothing is written and `recordStart` is not
  called. The final submit runs the existing gauntlet, then
  `canonicalAnswers`, `Path`, `OnPath`, and validates the path only. A
  problem is shown on the page that holds it. Both `/s/` and `/p/`.
- `internal/export`: `FormatVersion = 3`; `logic` on each exported
  question. `docs/export-format.md`: the field, the first match rule,
  and how an importer recomputes whether a question was shown.
- Tests: `internal/http/respond_logic_test.go` drives a published
  survey by plain POSTs: each branch reaches the right questions; a
  required question off the path is not asked for; a forged answer to an
  off path question is not stored; Continue writes no response and adds
  no start; a survey republished between pages still pins the served
  version; a localized option follows the same jump. A survey without
  logic renders the same page as before.
  `internal/store/immutability_test.go`: an `UPDATE` of `logic` is
  refused. The export test round trips a version with logic.
- Gallery: the respondent's first, middle and last page without
  JavaScript, a page re-rendered with a problem, in both languages.

### SL-3 Paging with JavaScript (M)

- `web/templates/respond.templ`: questions after the current page inside
  `<template class="js-respond-rest">`, and the version's jumps in the
  page's JSON block with option conditions as positions.
- `web/static/js/respond.js`: import the rest before the draft,
  versions, keyboard and voice wiring attach; Next follows the jumps;
  Back walks a history stack; inputs off the path are disabled so they
  are not posted; progress reads by position. Restoring a saved draft
  (story 81) re-evaluates the path.
- Tests: `e2e/tests/logic.spec.ts` runs the same branches with
  JavaScript on and off (a context with `javaScriptEnabled: false`)
  and asserts the same stored answers on the results page; keyboard
  answering crosses a jump.

### SL-4 Results, stats and insights read the path (M)

- `internal/store/results.go`: each response's version logic available
  to the view, so `viewQuestionResults` in `internal/http/results.go`
  counts skips among responses shown the question and says how many it
  was shown to. Percentages in distributions use the same denominator.
- `internal/http/statspage.go`, `web/templates/stats.templ`: a question
  that ends a branch is marked, and its stops read as finishes there.
- `internal/http/insights.go`: `insightPrompt` states how many
  responses each question was asked of.
- Tests: results and stats tests on a branched survey across two
  versions, one with logic and one without (pin don't copy still holds);
  the CSV leaves off path cells empty.
- Gallery: results and stats of a branched survey.

### SL-5 The flow, drawn (M)

- A Flow view on the editor: without JavaScript, an ordered list of
  questions, each with its jumps and its fall through as text.
  `web/static/js/flow.js` draws connectors as SVG over that list from
  the JSON block, as `stats.js` draws its chart; no library, no CSP
  change.
- `docs/style-guide.md`: the connector and the diagram, in both modes,
  with reduced motion respected.
- Tests: the list's text at the edge; a Playwright check that the SVG
  draws one connector per jump. Gallery: the flow of a linear survey and
  of a branched one, at 390px and 1280px.

### SL-6 Drag and drop (M)

- Drag a question card to reorder: posts `questionMove` with a new
  `to` position beside `direction`; `Draft.MoveTo`. Up and Down stay.
- Drag from a question's connector onto a later question: opens that
  question's add jump form with the destination filled in, and the
  creator chooses the condition and saves. The drop never writes on its
  own.
- Keyboard and single pointer users keep the forms (WCAG 2.5.7).
- Tests: Playwright drags reorder and prefill; the same outcome by
  forms is already covered.

### SL-7 Generation proposes logic (S)

- `generateSystemPrompt` in `internal/http/generate.go`: each line may
  carry a `ref` and `logic` naming refs within the same run.
  `appendGenerated` maps refs to new identities, validates with
  `LogicProblems`, drops what fails and counts it in the notice.
- Tests: the scripted provider returns a run with a valid and an
  invalid jump; the draft holds the first and the notice counts the
  second.

## Out of this plan

Conditions on answers to earlier questions, a custom ending per branch,
piping an answer into a later question's text, and per path counters
(ADR-0009's list is exhaustive). Each is an amendment to ADR-0020 or a
new ADR.

## Open questions for the owner

1. Jumps, not show if conditions, as the model creators write and the
   diagram draws. Agreed?
2. Conditions on the question's own answer only, for now. Typeform and
   Google Forms both started there; conditions on earlier answers would
   be a later amendment. Enough for the first release?
3. Options named by text, with a broken jump kept and marked when an
   option is renamed, rather than stable option ids across the schema.
   Acceptable, given that a rename then needs the jump fixed by hand?
4. Between SL-1 and SL-2, refuse publishing a draft with logic, or keep
   the editor behind a configuration flag until SL-2 lands?
5. In the CSV, an empty cell for both "skipped" and "not shown", or a
   marker for "not shown" that a spreadsheet then has to filter out?
6. Progress on a branched survey: "Question 7 of 10" by position (a
   jump makes it leap forward), or no total at all?
7. "Required" meaning "required when shown": confirm this is what a
   creator expects, and whether the editor should say so beside the
   checkbox.
8. Should a question that no path reaches block publishing, rather than
   only warn?
9. Story numbers 90 to 92 are proposed for SPEC.md; renumber if other
   work has taken them.
