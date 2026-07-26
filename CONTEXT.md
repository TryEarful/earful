# Earful

Open-source, AI-enhanced survey platform (Go + templ). Voice-first answering; trust, privacy and self-hostability are the differentiators.

This file fixes the vocabulary: one term per concept, used consistently in code, UI copy and documentation. How that vocabulary should be written — the register for comments and docs — is in [CONTRIBUTING.md](CONTRIBUTING.md#comments-and-documentation).

## Language

**Survey**:
The long-lived identity of a questionnaire — the thing a creator names, shares and sees results for. Structure lives in its versions.
_Avoid_: form, questionnaire

**Survey Version**:
An immutable snapshot of a survey's structure, created only on publish and never modified afterwards. The share link always serves the latest version.
_Avoid_: revision, edition

**Draft**:
The single mutable working copy of a survey. Accepts no responses; publishing freezes it into the next Survey Version.

**Draft Revision**:
An append-only record of one save of the Draft. Autosave history for editors — never respondent-facing.
_Avoid_: version (reserved for published snapshots)

**Audit Log**:
The derived who-changed-what-when view over draft revisions and publishes, visible to a survey's editors.

**Question Identity**:
The stable identity a question keeps across survey versions. Rewording preserves it; a genuinely new question gets a new one. Results aggregate across versions by question identity.

**Response**:
One respondent's submission to a survey, pinned permanently to the survey version they were served. Never copied or moved between versions.
_Avoid_: reply, submission

**Answer**:
The value a response holds for a single question.

**Workspace**:
The owning and billable unit. Every survey belongs to exactly one workspace; users are members. Signup auto-creates a personal workspace.
_Avoid_: team, organization, account

**Anonymous Survey**:
A survey whose responses carry no identifying data (no email, IP, or user-agent) — fixed at creation, unchangeable by any version.

**Invited Survey**:
A survey answered only via unique per-participant links; each response is linked to the participant's email.
_Avoid_: non-anonymous, private

**Participant**:
A person invited by email to an invited survey; holds one unique link.
_Avoid_: respondent (respondents include anonymous answerers)

**Respondent**:
Anyone answering a survey — a participant (invited) or an anonymous answerer.

**Closed**:
A survey past its close date (or manually closed): it accepts no new responses; results remain visible. Responses themselves are final at submission — there is no editing.
_Avoid_: archived, ended

**Close Date**:
Optional date after which a survey stops accepting responses. A survey-level setting, not part of the versioned structure — changing it creates no version.

**Transcript**:
The text produced from a respondent's spoken answer. The only artifact of voice input — audio itself is never stored. Editable by the respondent before submission.
_Avoid_: recording (recordings do not exist in Earful)

**Creator**:
A workspace member who builds and manages surveys.
_Avoid_: researcher, author

**Survey Status**:
Where a survey stands: Draft (never published), Open (published and accepting responses), or Closed (not accepting; reopenable). Distinct from Survey Versions, which only count publishes.

**Localization**:
The set of creator-reviewed, AI-drafted translations of a version's questions. Frozen into the Survey Version at publish — immutable like the rest of the version.
_Avoid_: translation (reserved for answer translation)

**Insight Summary**:
AI-generated themes, patterns and representative quotes across a survey's responses, aggregated by Question Identity across versions. Always labelled as AI output; never a substitute for the responses themselves.
