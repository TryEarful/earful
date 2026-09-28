# The feature tour, slide by slide

Each block below is one slide. The front matter is structure (an id, a layout, which screenshots); the body is the wording. Layouts: `cover` and `section` (a `###` kicker, a `#` title, prose, a list), `text` (a `##` title, an optional lead paragraph, then one column per `###` heading), `side` (title, prose on the left, one screenshot), `two` (title, a lead, two screenshots), `one` (title, lead, one wide screenshot), `code` (title, lead, one screenshot beside a file from out/). For layouts with screenshots, the last lines of the body are the captions, one `-` line per screenshot in order. Screenshot ids come from the build and screenshots stages (see out/shots.json after a run).

---
id: cover
layout: cover
---

# Earful

Open-source, AI-enhanced, voice-first surveys.

A tour of every feature, told through three stories. Screenshots from a local docker compose run.

---
id: what
layout: text
---

## What Earful is

Surveys people answer by speaking, analysed with AI, built so that trust is the product.

### Voice-first

Respondents can speak any open answer. The audio is transcribed in the same request and discarded: no recording exists, anywhere.

The transcript is editable before it becomes the answer. Typing is always available.

### AI-enhanced

AI drafts questions from a sentence, writes an Insight Summary over every response, translates answers and localizes questions.

Every model call goes through one seam. Ollama, llamafile, Vertex or any OpenAI-compatible server: configuration, not code.

### Trust by design

Anonymous means anonymous: no email, IP or device details are stored, and the database refuses to change that later.

Published versions are immutable, every workspace exports in one click, and the whole thing self-hosts with docker compose under AGPL-3.0.

---
id: stories
layout: text
---

## Three stories, every feature

Each story exercises a different part of the product.

### 1 · Bocca Lupa

A neighbourhood restaurant asks guests what they loved and what to add to the menu.

Anonymous public link, AI-drafted questions, a Spanish localization, translated answers, results and stats.

### 2 · Kettle & Crow Coffee

An online roaster gets to know its subscribers.

Invited survey with personal links, email invitations, per-participant results, small-sample privacy.

### 3 · Earful on Earful

The product team asks its own users how happy they are and what is missing.

Spoken answers, a reworded question across versions, the audit log, Insight Summaries over time.

---
id: home-trust
layout: two
shots: [home, trust]
---

## The front door and the trust page

Both public, both served by the instance that holds the data.

- The home page: one line and a sign-in button.
- The trust page states what is stored, what is not, and how to leave.

---
id: signin
layout: two
shots: [login, mailpit-magic-link]
---

## Signing in: a magic link, no password

Enter an email address, click the link. When self-hosting, the local inbox is mailpit; in production, an email provider.

- The sign-in page. Google login appears when configured.
- The link lands in the inbox.

---
id: confirm
layout: two
shots: [confirm-signin, dashboard-empty]
---

## Confirm, and land on the dashboard

The link only signs you in on a button press, so an email scanner that follows links cannot burn it. A personal workspace is created on first sign-in.

- The confirmation page.
- An empty workspace.

---
id: story-restaurant
layout: section
---

### Story 1

# Bocca Lupa asks its guests

A small Italian restaurant prints a QR code on the bill. Guests speak English, Spanish and Italian, and the owner wants to know what they loved, how they found the place, what to add to the menu, and where service slips on weekends.

- An anonymous survey anyone with the link can answer
- Questions drafted by AI, then edited
- A Spanish localization, drafted by AI and reviewed by a person
- Results, an Insight Summary, translated answers, and stats over a month

---
id: new-survey
layout: side
shots: [new-survey]
---

## Create a survey: the anonymity promise is permanent

A survey is anonymous or invited from the moment it is created, and that choice can never be changed. Not by the creator, not by the operator.

The database enforces it with a trigger, so no later feature or admin script can quietly attach identity to an anonymous response.

An optional close date stops the survey automatically.

- The form. The choice is explained where it is made.

---
id: draft
layout: two
shots: [ai-draft-prompt, ai-draft-streaming]
---

## Draft the questions with AI

Describe what you want to learn. Questions stream in one per line and land as ordinary draft questions to edit, reorder or delete.

- The prompt, in the creator's own words.
- Questions arrive as they are written.

---
id: editor
layout: side
shots: [editor-questions]
---

## Eight question types, one editor

Long text and short text (both can be spoken), single choice, multiple choice, dropdown, rating scale with custom bounds, Net Promoter Score, and yes / no.

Each question can be required. Questions are reordered with Move up and Move down, edited in place, and deleted.

Every save is a draft revision, so nothing edited is ever lost.

- The draft after AI drafting: nine questions, ready to edit.

---
id: languages
layout: two
shots: [languages-drafted, languages-reviewed]
---

## Languages: AI drafts, a person reviews

Add a language code and the model drafts each question. Publishing is blocked until every translation has been read and saved.

- Nine translations drafted, none reviewed yet.
- Reviewed, and now part of the next published version.

---
id: publish
layout: two
shots: [card-sharing, card-versions]
---

## Publish: an immutable version and a share link

Publishing freezes the draft into version 1. Respondents always get the latest version; what they saw can never be edited afterwards.

- The share link and the close button.
- Every version, who published it, and when.

---
id: preview
layout: two
shots: [card-settings, preview]
---

## Settings, and a preview through the real renderer

Title and close date live outside the versioned structure. The preview renders the draft exactly as a respondent will see it; submitting it writes nothing.

- Settings, and the permanent anonymity note.
- Preview as respondent.

---
id: respond-intro
layout: two
shots: [respond-restaurant, respond-restaurant-thanks]
---

## Who runs the survey, and what happens to the answers

The opening names the workspace, states whether the survey is anonymous, explains voice, and links to the trust page. Responses are final at submission.

- The opening of every anonymous survey.
- After submitting.

---
id: respond-steps
layout: two
shots: [respond-restaurant-food, respond-restaurant-menu]
---

## Answering: one question at a time

Progress, keyboard shortcuts (Enter for next, letters for options), and unsent answers kept in the browser so a refresh loses nothing.

- A rating scale.
- Multiple choice, with letter shortcuts.

---
id: respond-nps-phone
layout: two
shots: [respond-restaurant-nps, respond-restaurant-phone]
---

## Net Promoter Score, and the same page on a phone

Respondent pages load no analytics, fonts or third-party scripts.

- The NPS question, 0 to 10.
- Phone width.

---
id: respond-es
layout: side
shots: [respond-restaurant-es]
---

## In the respondent's language

A language picker offers the published localizations. The choice travels in the URL and is stored nowhere: no cookie, no column, nothing that could later say which language a person reads.

- The same survey in Spanish.

---
id: results-rating
layout: two
shots: [result-restaurant-food, result-restaurant-heard]
---

## Results: every question type gets the right view

Distributions per question, with counts and percentages. Sixty-four guests answered over a month.

- A rating scale with its average.
- Single choice.

---
id: results-nps
layout: two
shots: [result-restaurant-nps, result-restaurant-menu]
---

## Net Promoter Score and multiple choice

NPS is computed the standard way: promoters minus detractors. Multiple-choice percentages are per respondent, so they need not sum to one hundred.

- NPS from 64 answers.
- The menu wishlist.

---
id: results-text
layout: side
shots: [result-restaurant-enjoy]
---

## Open answers, in the respondents' own words

Long-text and short-text answers are listed with their submission time and the version they answered.

Nothing is summarised here; this is the record.

Spanish and Italian answers sit next to English ones. The next slides show what AI does with them.

- The first open question, 64 answers.

---
id: insights
layout: two
shots: [insights-restaurant-streaming, insights-restaurant-done]
---

## Insight Summary: AI reads every answer

One click sends every response to the configured model and streams back themes, patterns, verbatim quotes and suggestions. The result is stored with its model and timestamp, and re-used until new responses arrive.

- Streaming in.
- Labelled as analysis, not data.

---
id: translate
layout: two
shots: [translate-restaurant, result-restaurant-enjoy-translated]
---

## Translate answers, keep the originals

Type a language code and every text answer is machine-translated, cached per answer, and shown beneath the original. The original is never touched, and an answer already translated is never sent to the model twice.

- The translate control on the results page.
- Each translation is marked and names the model.

---
id: stats-1
layout: two
shots: [stats-restaurant-big-picture, stats-restaurant-trend]
---

## Stats: the big picture and the trend

Opens, submissions, completion rate and median time to complete, over any date range. Counters are per survey and per day; none is linked to a response.

- All time.
- Submissions per day, or opens.

---
id: stats-2
layout: two
shots: [stats-restaurant-questions, stats-restaurant-audience]
---

## Where answers stop, and who the audience is

The last question each submission answered, keyed by question identity so rewording never shifts the counts. Audience groups with fewer than five responses are hidden.

- Question by question.
- Browser, device and country, as undated totals.

---
id: csv
layout: code
shots: [results-restaurant-table]
code: results/restaurant.csv
lines: 6
---

## The raw data: a table, and a CSV

Every response as a row. The CSV carries the same columns and is what the workspace export bundles per survey.

- All responses, expandable on the results page. Each row can be deleted.

---
id: story-coffee
layout: section
---

### Story 2

# Kettle & Crow Coffee meets its subscribers

An online roaster ships a bag a month. The team wants to know how subscribers brew, which roast they reach for, why they signed up, and what to fix about delivery. They want to know who said what, so the survey is invited.

- Participants added by email, each with a personal link
- Invitations sent from the app, one submission per person
- Results tied to participants, with the same charts and Insight Summary
- Small-sample privacy: audience groups under five stay hidden

---
id: invite
layout: two
shots: [participants-coffee, invite-email]
---

## Invite by email

Paste addresses or upload a CSV. Each participant gets a unique link; the app sends the invitations under an hourly cap to protect deliverability.

- The participant list shows who has submitted.
- The invitation as it arrives.

---
id: invited-respond
layout: two
shots: [respond-coffee, respond-coffee-already]
---

## A personal link, one submission

The link is the credential: no account, no password. It works exactly once.

- The invited survey names the workspace and says the answers are linked to the invitation.
- Opening a used link again.

---
id: coffee-results
layout: two
shots: [result-coffee-brew, result-coffee-roast]
---

## What subscribers said

Seven of eight participants answered.

- How they brew.
- Which roast.

---
id: coffee-results-2
layout: two
shots: [result-coffee-why, result-coffee-cup]
---

## Why they subscribed, and their best cup

Open answers on an invited survey carry the participant's address.

- Multiple choice.
- Open answers, each with who wrote it.

---
id: coffee-insights
layout: side
shots: [insights-coffee-done]
---

## Insight Summary for the roaster

The same feature reads seven answers as easily as sixty-four, and says so when a sample is too small to support a conclusion.

The summary is stored with the response count it read, so a re-run after new answers is clearly a new reading.

- Labelled with the model and the count.

---
id: coffee-privacy
layout: two
shots: [results-coffee-table, stats-coffee-audience]
---

## Who answered what, and what stays hidden

Invited surveys link every response to a participant. Even here, the audience counters are unlinked totals and any group under five is suppressed.

- The response table.
- Only groups of five or more are shown.

---
id: story-earful
layout: section
---

### Story 3

# Earful asks its own users

The product team wants to know how satisfied creators are, which features they use and what is missing. They also want people to talk rather than type, and they reword a question after the first week without losing a single answer.

- Spoken answers with consent, transcription and an editable transcript
- A question reworded and published as version 2
- Results folded across both versions by question identity
- The audit log, trends and drop-off

---
id: voice
layout: two
shots: [voice-consent, voice-recording]
---

## Answer by speaking

Consent first. Audio streams to the server over a WebSocket, is transcribed, and is discarded in the same request.

- The consent dialog states the promise before the browser asks for the microphone.
- While recording, a level meter shows which input is live.
- Hold Space to talk and release to transcribe; Esc twice starts the answer over.

---
id: voice-transcript
layout: side
shots: [voice-transcript]
---

## The transcript, ready to edit

The words land in the same text box a typed answer would use, with a note that they were transcribed.

The respondent can correct anything before moving on. Typing stays available at any time.

No recording exists: not on disk, not in a database, not in a log, in any environment.

- The transcript in the answer box.

---
id: reword
layout: two
shots: [question-editor-open, card-two-versions]
---

## Reword a question, publish version 2

Editing a published question keeps its identity. Version 1 stays exactly as respondents saw it; version 2 goes live on publish.

- Each question opens into its own editor.
- Two immutable versions.

---
id: audit
layout: side
shots: [audit]
---

## The audit log

Every draft save and every publish, with who and when.

Derived from the append-only draft revisions, so it cannot be rewritten.

- The audit log of the Earful survey.

---
id: fold
layout: two
shots: [result-earful-nps, result-earful-sat]
---

## Results fold across versions

Answers to both wordings are counted together because they are the same question. The old wording is one click away.

- NPS across v1 and v2, with both wordings.
- A 1 to 10 rating scale.

---
id: earful-results
layout: two
shots: [result-earful-feat, result-earful-missing]
---

## Which features they use, and what is missing

Feature usage from a multiple-choice question, and the open answers that name the gaps.

- Feature usage.
- Open answers, spoken and typed.

---
id: earful-insights
layout: side
shots: [insights-earful-done]
---

## Insight Summary over both versions

The prompt carries every wording of a reworded question, so the model reads the history, not just the latest text.

Participant identity never enters the prompt.

- The summary over 41 responses.

---
id: earful-stats
layout: two
shots: [stats-earful-trend, stats-earful-questions]
---

## Trends and drop-off

Three weeks of submissions, and where answers stopped.

- Per day.
- Question by question.

---
id: story-operator
layout: section
---

### The operator's side

# Trust, export, administration

Everything a self-hoster or the Earful team needs to run the service honestly: a public trust page served by the instance that holds the data, a one-click export, founder metrics, GDPR erasure and private-beta controls.

- Export everything
- Metrics, erasure, invite codes
- Close and reopen
- Phone-friendly throughout

---
id: export
layout: side
shots: [export-ready]
---

## Export everything

One button builds an archive of every survey, version, question and response in the workspace: documented JSON plus a CSV per survey, with any Insight Summary beside it.

The archive is built in the background and the link expires after a day.

The format is published and versioned (docs/export-format.md), and Earful is AGPL-3.0, so the same software runs anywhere the archive goes.

- Ready to download.

---
id: admin-1
layout: two
shots: [admin-metrics, admin-erasure]
---

## Administration: metrics and erasure

Super-admin pages that do not exist as far as anyone else can tell.

- Founder metrics from the database, never from respondent pages.
- GDPR erasure: look up, see what would go, confirm.

---
id: admin-2
layout: two
shots: [admin-beta-codes, closed-editor]
---

## Private beta, and closing a survey

Invite codes gate signup during a private beta. A closed survey accepts no responses and keeps its results; reopening is one click.

- Invite codes for a private beta.
- The creator's view of a closed survey.

---
id: closed
layout: side
shots: [closed-respondent]
---

## What a respondent sees when a survey is closed

The page says so plainly and points them back to whoever sent the link.

- A closed survey.

---
id: phone
layout: two
shots: [phone-dashboard, phone-results]
---

## It works on a phone, for creators too

Every creator page is tested at phone, tablet and desktop widths.

- The dashboard.
- Results.

---
id: run-it
layout: text
---

## Self-hosting and AI providers

The whole stack runs from one command. AI is optional and pluggable.

### Run it

`docker compose --profile app up --build`: Postgres, migrations, the app on port 8080 and a local mail catcher on port 8025.

Or from source with `make dev`. AGPL-3.0, no secrets in the repo, twelve-factor configuration.

### Choose a model

`AI_PROVIDER`: none, openai (Ollama, llamafile, any compatible server), vertex, or scripted for development.

`TRANSCRIBE_PROVIDER` selects voice separately: whisper-cli on a laptop, Vertex in production.

A daily spend breaker and a per-workspace token cap guard the bill.

### About this deck

Every screenshot comes from a local compose run. The model behind the AI panels was a stand-in server returning prepared text, so the prose in the Insight Summaries and translations is illustrative.

Responses were seeded directly into the database to show a month of data.

---
id: end
layout: cover
---

# Earful

Open-source, AI-enhanced, voice-first surveys.

github.com/TryEarful/earful · AGPL-3.0
