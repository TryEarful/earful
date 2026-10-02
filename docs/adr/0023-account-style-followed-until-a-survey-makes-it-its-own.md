# An account's style is followed by its surveys until a survey makes a part its own

Status: proposed. Amends ADR-0018 and narrows ADR-0019.

A creator can set a style once, on the account, and every survey of the
workspace uses it: the theme, the header (banner, logo, name, tagline
and links), the footer and the thanks page's picture, the same choices a
survey's Style tab offers (ADR-0018). A survey follows the account's
style part by part until its creator changes a part on its own Style
tab, which makes that part the survey's own. Two boxes on the Style tab,
"Custom header" and "Custom footer", take the header or the footer off a
single survey. A published version keeps the look it was published
with: a change to the account's style reaches a survey's respondents
when that survey is next published, and saving the account's style
offers to publish the change to the active surveys that show it.

## Context

ADR-0018 gave each survey a style of its own. A creator who runs many
surveys for one organisation sets the same logo, theme and footer on
every one, and changes each of them again when the organisation's
address changes. ADR-0018 named a workspace wide default style as a
separate need and left it to Survey Defaults (ADR-0019), which proposed
copying values into a new draft when it is made. A copy does not help
with the second half of the problem: once copied, a change to the
account reaches no survey.

What constrains the answer:

- **What a respondent was shown is immutable** (ADR-0001, ADR-0018). A
  version freezes the style it was published with, so the preview shows
  a look before it goes live, a respondent never sees the page change
  halfway through, and the export says what each respondent saw.
- **A version only refers to its survey's pictures.** The public picture
  address serves a picture while a version of the same survey shows it,
  the `survey_images` trigger refuses to delete one a version shows, and
  the export and the purge read pictures per survey (ADR-0018).
- **Translations are reviewed before they are published.** A survey
  cannot be published while a language it carries has unreviewed words,
  the style's included (ADR-0018).
- **A workspace is the unit of ownership, export and purge** (ADR-0002,
  ADR-0010). In practice a workspace has one member, so the account's
  style and the workspace's are the same thing to a creator.
- **Every page works without JavaScript**, and a page has one filled
  button (ADR-0016).

## Decisions

**The account's style is the workspace's.** It is kept in
`workspace_styles`, one row per workspace, beside its words in each
language, and edited at `/account/style`, a page of the account with the
Style tab's form and one filled "Save account style". A workspace with no
row has no account style, and its surveys are drawn exactly as they were.

**A survey follows it part by part.** The draft records which of the four
parts (theme, header, footer, thanks picture) are the survey's own, and
holds values only for those. A part not marked as its own follows the
account. A draft saved before this change carries no marks: a part with
a value is its own, and an empty part follows the account, so a live
survey keeps its look and needs no migration of its data. An empty part
of such a survey takes the account's on its next publish, and its editor
says so.

**The Style tab shows the account's values, and editing one is the
override.** The form arrives filled with the style the survey would be
published with: its own parts, and the account's for the rest, the
account's theme preselected. On save, each part the form carries is
compared with the account's: equal, it keeps following; different, it
becomes the survey's own. A part is the unit, so changing the tagline
makes the whole header the survey's own, and a later change to the
account's logo does not reach it. A header that mixed a survey's name
with an account's logo would say two things about who is asking. The
help text says this, and a "Reset to account style" button, outlined,
at the end of the form, returns every part to the account's.

**"Custom header" and "Custom footer" take a part off one survey.** Both
are ticked by default. Unticked, the survey shows no header, or no
footer, whatever the account's style holds. The header's logo goes with
it, so Earful's owl returns to the footer. A hidden marker beside each
box tells an unticked box from a form drawn before the boxes existed,
and an unticked section's fields are not read.

**Publishing resolves the style and freezes it.** Inside the publish
transaction, with the survey and then the account's style held, the
survey's own parts are laid over the account's, the result is checked,
and it is frozen into `survey_versions.style` as before, with each
language's words drawn from the survey's translations for its own parts
and the account's for the rest. Each version also records which parts it
took from the account. Respondent pages, the pages of a closed survey,
the public picture address, the trigger and the export read the version
as they did.

**An account's pictures are copied into a survey that shows them.**
`workspace_images` holds the account style's pictures, re-encoded as a
survey's are, ten at most. When a survey is saved with a part that keeps
an account picture, or published with one it follows, the picture is
copied into its `survey_images` in the same transaction, under the same
limit. A version therefore only ever refers to its survey's pictures,
and the rules ADR-0018 set for them stand. The creator's preview finds
an account picture through the survey's own address, which looks in the
account's pictures as well.

**The account's words are translated once, on the account.** Each
language the workspace's surveys carry is offered on the account's
translations page, reviewed as a survey's words are and marked stale
when the account's wording changes. A survey that follows a part with
words cannot be published in a language whose account translation is not
reviewed and current; the editor says so and links to the account. A
survey's own parts are translated on its Languages tab as before. When a
part becomes a survey's own with words equal to the account's, the
account's translations come with it, reviewed if both were.

**The editor says when only the account's style has changed.** A survey
whose draft resolves to a style other than its live version's is
offered Publish, as for any change; where the account's style is the
only difference, the editor says "Your account style changed. Publish to
apply it."

**Saving the account's style offers to apply it now.** When an active
survey (published, open, not deleted) shows a part that has just
changed, the save lands on a page that names those surveys and offers to
update them. Updating publishes, for each, a new version made from its
live version with only the followed parts resolved again, through the
normal publish path: questions and translations are the live version's,
so unpublished edits in a survey's draft stay unpublished. A survey that
lacks a reviewed account translation for the changed part is named and
left for later. Versions published before this change record no
followed parts and are not offered; they take the account's style on
their next publish. Choosing "Later" changes nothing.

## Considered Options

- **Copy the account's style into each new survey** (ADR-0019 as
  proposed). Simple, and a survey never changes behind its creator's
  back. A change to the account, such as a new address, then has to be
  made in every survey by hand, which is the problem this solves.
- **Apply an account change to every live survey at once.** One step
  for the creator. It undoes ADR-0018's freeze: no preview before it is
  live, a respondent halfway through sees the page change, and a version
  no longer says what its respondents saw. Offering to publish the
  change keeps the choice and the freeze.
- **Follow field by field rather than part by part.** A survey could
  keep its own tagline under the account's logo. Each field would need
  its own mark, the form would need a control per field, and a header
  assembled from two sources could name one organisation beside
  another's logo.
- **Images owned by the workspace and referred to by versions.** No
  copies. Every rule ADR-0018 set for a version's pictures (the public
  address, the trigger, the export, the purge) would have to look in two
  places, and an account picture could not be deleted while any version
  of any survey showed it. A copy per survey is bounded by the ten
  picture limit and keeps those rules as they are.
- **Translate the account's words in each survey.** No new page. A
  change to an account word would make every following survey's
  translation stale, each to be reviewed again before it could be
  published.
- **A switch to turn the account's style off for an instance.** As in
  ADR-0018, there is nothing to configure and nothing to turn off.

## Consequences

- ADR-0018's "No switch for an operator" paragraph no longer sends a
  workspace default to Survey Defaults: the account's style is it. Its
  rejected option "Images owned by the workspace" stands for versions;
  an account's pictures are the workspace's, and are copied into a
  survey before a version shows them.
- ADR-0019 no longer covers style. Its defaults fill in forms; the
  account's style is followed, not copied. Proposal 19's slice SD-5 is
  replaced by this decision.
- Migration 00027 adds `workspace_styles` and `workspace_images`, whose
  rows are never rewritten. The purge erases both with their workspace,
  and an account picture its style no longer shows after seven days.
- The workspace export moves to format version 8: the workspace carries
  the account's style and its words, and its pictures are under
  `images/` with the versions'.
- Every survey that follows a changed part shows "changed" in its
  editor, and those with languages wait for the account's translation
  before they can be published. The wording on the editor, the account
  page and the confirmation page has to make that plain.
- An account picture is stored once for the account and once more for
  each survey that shows it, within each survey's limit of ten.
- A Style tab left open while the account's style changes makes the
  changed parts that survey's own when it is saved. With one member per
  workspace this is rare.
