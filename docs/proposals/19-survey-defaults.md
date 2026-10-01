# Survey defaults: build plan

Issue #19 asks for a creator profile that keeps preferences (question
types, style, fonts and brand) and reuses them when a new survey is
made. [ADR-0019](../adr/0019-survey-defaults-belong-to-the-workspace.md)
proposes what that becomes: **Survey Defaults**, kept per Workspace, that
fill in the new survey form and the add question form and never fill in
a field on the server. Brand, colour and typeface are left to the
branding decision proposed as ADR-0018.

This file is the plan for building it. Each slice ships on its own, in
order, and leaves `make check` and `make e2e-smoke` green. Tests are at
the application edge (`docs/testing.md`): a signed in `apptest` client
posts forms and reads pages; nothing reaches into a package.

## What a creator sees

- **Account** gains a card, "Survey defaults", with one line saying
  what they are for and a link to `/account/defaults`. The account page
  keeps its single filled button rule; the card holds an outlined link.
- **`/account/defaults`** is a page of its own: a stack of cards (New
  surveys, New questions, Drafted questions) in one form, with one
  filled button, Save defaults, and a note that a default only fills in
  a form and changes no survey already made.
- **New survey** opens with the default audience selected and, when the
  workspace has default languages, a fieldset "Also in" with each one
  ticked. The permanent audience warning stays where it is.
- **The editor's add question form** opens with the default type
  selected, "Required" ticked or not, and the default scale in its two
  number fields.

## Slices

### SD-1 Defaults for new questions and the audience (M)

The table, the page, and the two forms that read it. Export and purge
are in this slice, not after it: a setting a workspace cannot take with
it, or that outlives the workspace, should not ship even for a release.

- Migration `db/migrations/000NN_workspace_defaults.sql` (the next free
  number; 00018 at the time of writing, unless ADR-0017 or ADR-0018
  takes it first):

  ```sql
  CREATE TABLE workspace_defaults (
      workspace_id     uuid PRIMARY KEY REFERENCES workspaces (id),
      anonymous        boolean NOT NULL DEFAULT true,
      question_type    text NOT NULL DEFAULT 'long_text',
      required         boolean NOT NULL DEFAULT false,
      scale_min        int NOT NULL DEFAULT 1 CHECK (scale_min IN (0, 1)),
      scale_max        int NOT NULL DEFAULT 5 CHECK (scale_max BETWEEN 2 AND 10),
      updated_by       uuid REFERENCES users (id),
      updated_at       timestamptz NOT NULL DEFAULT now()
  );
  ```

  `question_type` is checked in Go against `domain.QuestionTypes`, not
  by a `CHECK`, so that adding a type is not a migration. The comment at
  the head of the file says why a missing row is the product's defaults.
- Queries in `db/queries/defaults.sql` (`GetWorkspaceDefaults`,
  `UpsertWorkspaceDefaults`), generated into `internal/store/db`.
- `internal/domain/defaults.go`: a `SurveyDefaults` value with
  `ProductDefaults()` and `Validate()`, reusing the scale bounds the
  question already enforces. Its error is a user error worded through
  `uitext`, as every validation error is.
- `internal/store/defaults.go`: `Defaults(ctx, workspaceID)` returns the
  product's values for a workspace with no row; `SaveDefaults` upserts
  and writes `updated_by`.
- `internal/http/pages.go`: `accountDefaultsPage` and
  `accountDefaultsSave`, routed in `internal/http/routes.go` beside
  `/account/workspace` as `get("/account/defaults", …)` and
  `post("/account/defaults", …)`. The workspace is always the session's.
  A save redirects with `?notice=defaults_saved`; an invalid value
  re-renders with a 422 and the error beside the field.
- `internal/http/surveys.go`: `renderNewSurvey` and `renderSurveyPage`
  read the defaults and hand them to the templates. `surveyCreate`,
  `questionAdd` and `questionFromForm` do not change.
- `web/templates/app.templ`: the account card and `AccountDefaults`.
  `web/templates/surveys.templ`: `NewSurvey` takes the audience
  default; `addQuestionForm` takes the type, required and scale.
- Wording in `web/text/active.en.toml` and `active.es.toml` under
  `account.defaults.*`.
- Export: `internal/export/export.go` gains `Workspace.Defaults`;
  `internal/http/export_workspace.go` fills it; `FormatVersion` becomes
  3; `docs/export-format.md` documents the object and the bump.
- Purge: a `defaults_of_deleted_workspaces` step in
  `internal/purge/purge.go`, before `deleted_workspaces`, and a
  `detach_purged_users_from_defaults` step beside
  `detach_purged_authors_from_drafts`, so that a purged member who last
  saved a live workspace's defaults leaves them attributed to nobody.
- `CONTEXT.md`: **Survey Defaults**.

Tests:

- `internal/http/defaults_test.go`: a new workspace's page shows the
  product's values; saving "invited", `nps` and required, then opening
  `/surveys/new` and an editor, shows the invited radio checked, NPS
  selected and Required ticked; posting the new survey form exactly as
  rendered makes an invited survey, and posting it with "anonymous"
  makes an anonymous one whatever the default (the form decides); an
  unknown type and a scale of 0 to 11 are refused with the old values
  kept; a second workspace's pages are unaffected by the first's save;
  the page and the save need a session and a CSRF token.
- `internal/http/export_workspace_test.go`: `format_version` is 3 and
  `workspace.defaults` round trips, both for a saved row and for none.
- `internal/purge/purge_test.go`: a deleted workspace's row is gone
  after the window and kept within it; the dry run counts it.
- `internal/http/surveys_test.go` passes unchanged, which is the proof
  that the handlers did not.

Gallery (`e2e/gallery/gallery.spec.ts`): `account` (with the new card),
`defaults` (product values), `defaults-saved` (notice), `defaults-error`
(an invalid scale), `survey-new-defaults` (invited preselected) and
`editor-draft-defaults` (NPS selected). Reviewed against the style
guide's checklist by the reviewer agents, per CLAUDE.md.

### SD-2 Default languages (S)

- Migration: `ALTER TABLE workspace_defaults ADD COLUMN languages text[]
  NOT NULL DEFAULT '{}'`, with a `CHECK` on `cardinality(languages) <=
  10`, matching `maxLanguagesPerSurvey`.
- `domain.SurveyDefaults.Validate` applies `ValidLang` and
  `NormalizeLang` to each and refuses duplicates.
- The defaults page gains a field for languages, as the localizations
  page asks for one: the same input and the same validation messages.
- `NewSurvey` renders an "Also in" fieldset of checkboxes, one per
  default language, ticked. Unticking one leaves it out of this survey.
- `surveyCreate` reads the posted `languages` values; `store.Surveys.Create`
  takes them and calls `Draft.AddLanguage` on the empty draft before it
  is encoded, in the same transaction. An invalid posted language is a
  user error on the form, as on the localizations page.
- Export: `languages` in the `defaults` object (still the same new format version
  if SD-1 and SD-2 ship in one release; 4 otherwise).

Tests: with Spanish as a default, the new survey form shows it ticked; a
survey made from that form lists Spanish on its languages page as
unreviewed; adding a question and publishing is refused until the
Spanish is reviewed (story 23), then succeeds; unticking it makes a
survey with no languages; a workspace with no default languages renders
no fieldset at all.

Gallery: `survey-new-languages`, `defaults` with a language listed.

### SD-3 Guidance for drafted questions (S)

The issue's "style", in the one place a style can act today: the
questions the AI drafts.

- Migration: `ADD COLUMN guidance text NOT NULL DEFAULT ''` with a
  `CHECK (char_length(guidance) <= 500)`.
- `generateSystemPrompt` in `internal/http/generate.go` takes the
  guidance and appends it, after the rules for the reply's shape, under
  a line saying it is the creator's guidance on tone and wording. Both
  the plain POST and the socket path read it.
- `recordGeneration` counts its characters with the prompt's, so the
  quota (story 21) covers it.
- The defaults page gains a textarea with a counter, and a sentence
  saying the guidance is sent with every drafting request to the model
  that drafts, processed in the EU. The field is shown only where
  drafting is available (`canGenerate`).
- Export: `guidance` in the `defaults` object.

Tests with `apptest.Options{AI: fake}`: after saving guidance, a drafting
POST sends a system prompt that contains it, and one without guidance
sends exactly today's prompt; the usage recorded grows by its length;
501 characters are refused; on an instance without AI the field is not
drawn and a posted value is ignored.

Gallery: `defaults` with drafting available and without.

### SD-4 Start from a copy of a survey (M)

The quicker start the issue describes, without templates (ADR-0019,
Considered Options).

- `post("/surveys/{surveyID}/copy", s.surveyCopy)` renders the new
  survey form with the title "Copy of …" (a message, not a string in
  code), the audience radio set from the workspace's defaults rather
  than the source, and a hidden `source` field.
- `surveyCreate` with a `source` from the session's workspace copies
  the source's current Draft into the new one: questions with fresh
  Question Identities, and its languages with their translations marked
  unreviewed. A `source` from another workspace is the same 404 as any
  survey from another workspace.
- The editor gains an outlined "Make a copy" button beside Preview.
  The audit log of the new survey records that it was copied, and from
  which survey title.
- `surveys.origin` stays `creator`.

Tests: a copy has the source's questions in order, none of its Question
Identities, its languages unreviewed, no responses, no versions and no
participants; copying an anonymous survey into an invited one is
allowed and the source is untouched; copying a survey from another
workspace is a 404; the Starter Survey can be copied.

Gallery: `survey-copy` (the form, prefilled).

### SD-5 Brand defaults (L, waits on ADR-0018)

Whatever ADR-0018 decides a brand is, it is a workspace property kept
beside these defaults, exported in the same object and purged in the
same step. No typeface is added by this plan. This slice is written once
ADR-0018 is accepted.

## Open questions for the owner

1. **Workspace or user.** ADR-0019 puts defaults on the workspace. The
   issue says "personal profile". In a one member workspace these are
   the same; confirm the workspace is right before member invites make
   the difference visible.
2. **Where the page lives.** A card that links to `/account/defaults`,
   or the fields directly on the account page as more cards. The account
   page already holds four cards and the delete action.
3. **Should the audience have a default at all.** Preselecting
   "invited" is a convenience; leaving the anonymity choice with no
   default, so it is always clicked, is the stricter reading of
   ADR-0003. The form proposed here preselects and keeps the warning.
4. **Copies and reviewed translations.** SD-4 marks a copy's
   translations unreviewed, so the copy cannot be published in another
   language without a read. The wording is identical, so keeping them
   reviewed is defensible; which is wanted.
5. **Saved templates.** Copying a survey is proposed in their place.
   If creators want a named library of starting points that are not
   surveys, that is a decision of its own, with its own export and purge.
6. **The language a creator writes in.** Nothing records it (ADR-0014),
   so dictation on an untranslated survey listens for no language. A
   workspace default "surveys are written in" would fix that, and is a
   change to a survey level fact first. In scope here, or its own ADR.
7. **Typefaces.** Confirm that a custom font is out of scope for this
   issue and belongs to ADR-0018, and whether ADR-0018 should consider
   only open licence faces bundled with Earful.
8. **Release grouping.** SD-1 and SD-2 in one release keeps the export at
   one format version with both; shipping them apart means two.
