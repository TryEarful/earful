# Survey defaults belong to the workspace, and only fill in a form

Status: proposed

A Workspace may keep Survey Defaults: the choices a creator would
otherwise make again on every new survey. They are a row of their own
beside the workspace, `workspace_defaults`, one per workspace, edited
from the account page. A workspace with no row behaves exactly as every
workspace does today.

A default does one thing: it decides what a form shows before the
creator touches it. The new survey form opens with the default audience
selected and the default languages ticked; the add question form opens
with the default type, the default scale and the default "required".
What is created is what the form posts. No handler reads a default to
fill in a field a form left out, so a survey is never made from a
setting its creator did not see on the page.

The defaults this records:

| Default | Applied to | Product value when unset |
|---|---|---|
| Audience, anonymous or invited | the new survey form | anonymous |
| Languages to translate into | the new survey form, as ticked boxes | none |
| Question type | the add question form | the first in `domain.QuestionTypes` |
| Required | the add question form | not required |
| Scale, lowest and highest point | the add question form | 1 to 5 |
| Guidance for drafted questions | the system prompt of question drafting | none |

A brand, meaning a theme, a header, a footer or a logo on a survey's
pages, is not one of them. ADR-0018, accepted, makes it a survey's
Style, chosen on its Style tab and frozen with each version. A
workspace's default style, which a new survey would start from, is a
property of the workspace beside these, under the same rules for
export and purge; it copies a style into a new draft and needs no
change to how a survey is drawn.

## Why the workspace, not the user

Surveys belong to workspaces (ADR-0002), and so does everything a
respondent reads about who is asking: the workspace's name is already
set from the account page (story 89). A default is a fact about how a
workspace makes surveys, not about the person signed in.

While a workspace has one member the difference cannot be seen: the
creator edits "my defaults" and gets them. It shows when members
arrive. Two members of one workspace making surveys that look and ask
alike is the purpose of a shared default; two members each with their
own audience default is a way for an invited survey to be made where
the workspace's surveys have been anonymous.

The workspace is also the unit that is exported (ADR-0010) and purged,
so a default kept there leaves with the export and goes with the
workspace, with no new path for either.

## Why a form, not a fallback

Whether a survey is anonymous is fixed when it is created and can never
change (ADR-0003). That promise is kept by a creator choosing, on a page
that says the choice is permanent. A default that is applied on the
server, after the form, would make the most consequential choice in the
product one that was made once, on another page, perhaps by another
member. Pre-selecting the radio button keeps the choice and the warning
where they are, and saves only the click.

The same rule makes the feature work without JavaScript, since a
default is an attribute in rendered HTML (`checked`, `selected`,
`value`), and keeps every existing test of `surveyCreate` and
`questionAdd` true: their inputs are unchanged.

Languages are the one default that writes something at creation: the
ticked languages are added to the new survey's empty Draft in the
transaction that creates it, as `localizationAdd` would add them one by
one. They start unreviewed, and publishing waits for each to be read as
it does today (story 23). Nothing about a Localization changes.

## Considered Options

- **Preferences on the user.** Reads as the issue asks ("a personal
  profile"), and is what a creator in a one member workspace would
  expect. Rejected for the reasons above: it divides the workspace's
  surveys by who made them, and it sits outside the export and purge
  paths, both of which are scoped to the workspace. A per member layer
  can be added over the workspace's later, if members ask, without
  moving anything.
- **Both, with the user's overriding the workspace's.** Two places to
  look for one setting, and an order between them to explain, for a
  workspace that today has one member. Not done now; the table chosen
  does not prevent it.
- **A `jsonb` column on `workspaces`.** One migration and no join. But
  every key would be validated only in Go, the database could not refuse
  an unknown question type or a scale outside 0 to 10, and the export
  would carry whatever the column held. Typed columns with `CHECK`
  constraints, in a table of their own, keep `workspaces` as it is and
  let a missing row mean "the product's defaults", so nothing is
  backfilled.
- **Applying defaults on the server.** Shorter forms, and the anonymity
  problem described above. Rejected.
- **Saved templates: a set of questions kept apart from any survey,
  to start new surveys from.** This is what "create new surveys
  quicker" most often means elsewhere. A template is a second kind of
  survey shaped object, with wording, translations and an export and
  purge story of its own. Copying a survey the workspace already has
  gives the same start with nothing new to keep: the copy is an
  ordinary survey with a Draft of the source's questions, fresh Question
  Identities (identities belong to one survey, ADR-0001), and an
  audience chosen anew on the new survey form. It is proposed as a
  separate slice and needs no change to this decision. The Starter
  Survey is already such a source (ADR-0015).
- **A typeface of the creator's choosing.** From a font service it is a
  third party request on every respondent's page, which ADR-0006 forbids
  and ADR-0016 declined for Manrope itself. As an uploaded file it is
  served from Earful's own origin and passes `font-src 'self'`, but a
  font is parsed by the respondent's browser, a webfont licence is the
  uploader's to hold and Earful's to serve, and each upload is bytes
  kept per workspace. ADR-0018 adds no typeface either, and this
  decision adds none.
- **The interface language on the account.** ADR-0014 considered and
  declined it, and nothing here reopens that. The language a creator
  writes surveys in is a different fact, which nothing records today; it
  is listed as an open question in the proposal rather than decided
  here.

## Consequences

- The workspace export moves to its next format version, with a `defaults`
  object under `workspace`, documented in `docs/export-format.md`. A
  workspace with no row exports the product's values, so an importer
  never has to guess. The guidance is the workspace's own words and is
  exported as written.
- The purge removes a deleted workspace's row in the same pass that
  removes the workspace, as a named step beside
  `memberships_of_deleted_workspaces`, and detaches a purged user from
  the defaults they last saved, as it does from a Draft. Account
  deletion needs nothing new: it soft deletes the workspace already.
- A default question type is checked against `domain.QuestionTypes` when
  it is saved and again when the form is drawn. A type removed from the
  product falls back to the first in the list instead of failing the
  page.
- The guidance for drafted questions is creator text sent to the model
  with every drafting request, so it is processed where every AI call is
  (ADR-0011, ADR-0013), counted against the workspace's quota with the
  prompt, and bounded in length. It is appended to the system prompt as
  the creator's, below the rules for the reply's shape, so it cannot ask
  for a type the product does not have: the parser drops those whatever
  the prompt says (story 20).
- Respondent pages do not change. Nothing a default holds is read on
  `/s/` or `/p/`, so ADR-0006 and the CSP in `internal/http/security.go`
  are untouched, and no default can identify a respondent (ADR-0003).
- The Starter Survey is written before a workspace can have defaults
  and does not read them (ADR-0015).
- `CONTEXT.md` gains **Survey Defaults**, with "preferences", "profile"
  and "template" to avoid.
- Amends no ADR. It extends the export contract that ADR-0010 and
  `docs/export-format.md` define, and leaves a survey's style to
  ADR-0018.
