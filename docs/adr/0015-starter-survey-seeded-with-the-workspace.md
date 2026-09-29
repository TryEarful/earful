# A workspace is created holding a published survey, which is its owner's

Every workspace is created with one survey already in it: the Starter
Survey. It is published, anonymous and open, it asks five questions
about Earful, and it has an address of its own that anyone can answer
at. It is written in the transaction that creates the workspace, from
one definition, and from then on it is an ordinary survey. Its owner
rewords it, publishes it again, closes it or deletes it as they would a
survey they had made, and its answers arrive in their workspace and
nowhere else.

The survey is written in English and published with a Spanish
Localization, whatever language its owner signed up in. Its wording is
in `web/text`, under `starter`, beside the interface's.

`surveys.origin` records that a survey was seeded rather than made.
Nothing about how a survey behaves depends on it.

## Why

A workspace with nothing in it shows a creator an empty list and a
button. What a published survey looks like to a respondent, which is
what the product is, is several steps away: a title, questions, a
publish, a link opened in another window. A survey that is already
there is that page one click from the dashboard, and an example of a
survey worth answering that can be taken apart.

It asks about Earful because a survey about nothing is not an example
of anything. The same five questions, published from Earful's own
workspace, are how Earful asks its creators how it is doing.

## Considered Options

- **Seeding after the workspace is committed**, and carrying on if it
  fails. A workspace could then exist without its survey, for a moment
  or for good, and "every workspace" would need a job that looks for
  the ones left out. In the transaction it is every workspace or the
  signup did not happen. The cost is that a fault in the survey is a
  fault in signing up, which is why the definition is tested as the
  editor would test it and every test that signs in exercises it.
- **Seeding when the dashboard is first drawn.** A write on a read, and
  a survey that a creator who went straight to `/surveys/new` would
  find later, below their own.
- **One survey owned by Earful, linked from every workspace.** It would
  collect feedback, and it would show a creator nothing of their own:
  no results page, no editor, nothing to reword. It is also the first
  thing in the product to point from one workspace into another
  (ADR-0002).
- **The survey in the language its owner signed up in**, as the
  workspace's name is. One language each, and nothing to read again
  after rewording. Rejected because the address would then mean a
  different survey depending on who made the workspace, and because a
  survey in two languages is part of what there is to show: the picker,
  the Spanish buttons, dictation listening for Spanish.
- **The wording in the code.** The definition does not change with the
  request, so nothing requires a message for it. It is in `web/text`
  because that is where wording is rewritten (story 85), and because
  the checks on that file then cover the survey: a question with no
  Spanish fails the build, and `make text-status` lists the Spanish
  made from English that has since changed.
- **Telling a Starter Survey by its title.** The owner is invited to
  change the title.
- **Giving existing workspaces one.** A survey appearing in a workspace
  whose owner did not put it there is a different thing from a
  workspace that began with one. `earful starter-survey add` gives one
  to a workspace whose owner asks.

## Consequences

- The founder metrics count a Starter Survey from its second version,
  the first a person published. Counted from the first, Surveys and
  Published surveys would each rise by one with every signup and say
  nothing about what creators do.
- Rewording a question makes its Spanish a translation of something
  else, and publishing is refused until the Spanish is read again or
  the language is removed (story 23). This is the rule for every
  survey, met here by a creator who did not add the language.
- A survey's title is in one language, so a respondent reading the
  survey in Spanish reads an English title above it. `starter.title`
  has a Spanish message because every message has one; nothing shows
  it.
- The survey is published in Spanish and no other language the
  interface may come to be written in. A Localization is published as
  reviewed, and a language added to the interface has been read as
  interface text, not as a survey. Adding one is a change to
  `internal/starter`.
- The draft is saved and version 1 published in the owner's name, and
  the Audit Log says so. An actor it cannot name is shown as a deleted
  account, which would be the wrong thing to read on a survey's first
  line.
- The workspace export carries the survey like any other and does not
  carry `origin`, which is a fact about this instance. The format stays
  at version 2.
- The survey accepts dictation on its first question, so every
  workspace has an address at which its voice and AI allowance can be
  spent. The limits on any published anonymous survey apply to it.
- `internal/auth` creates the workspace and knows nothing of surveys.
  What a workspace starts with is a function handed to it
  (`auth.WorkspaceSeeder`), which also keeps `internal/store`, which
  imports `internal/auth`, out of its imports.
- A workspace holds at most one live Starter Survey, by a partial
  unique index. Restoring a deleted one by hand while another is live
  is refused by the database.
