---
title: How Earful treats your data
short_title: Trust
sections: cards
hash: sha256-a8b940f7f78938174f5856b135c7f796edfac786bc982e4456c2456d632e104d
last_update: 2026-09-29
---

This page describes {{if .Instance}}{{.Instance}}{{else}}this instance{{end}}. Earful is open source (AGPL-3.0), so every claim here can be checked against the code that serves it.

## Your voice is never stored

You can answer by speaking. The audio is turned into text and discarded in the same request — it is never written to disk, to a database, to logs, or to any backup, in any environment. What is kept is the transcript you read and edited before submitting.

There is no playback feature, because there is nothing to play back. If that ever changed it would be a different product decision, announced here and asked of you first — not something that could happen quietly to audio we had kept in the meantime.

## Anonymous means anonymous

A survey's creator chooses at creation whether it is anonymous, and that choice can never be changed afterwards — the database refuses it, not just the application.

An anonymous response carries no email address, no IP address and no device details. Those columns do not exist anywhere near a response, so no query and no mistake can quietly fill them in. Adding them would take a deliberate change to the database, in public, in an open-source repository.

Survey creators do see coarse counts about their audience — browser family, device type and country — as totals for the survey, never attached to any response, and hidden entirely for any group smaller than five people. Country is worked out on our own server from an offline database and the IP address is discarded immediately. How many times a survey was opened and how many answers were submitted are also counted per day, so a creator can see how a survey went over a week; those are counts of the survey, with nothing attached to any response, and the audience counts are never split by day.

## Nothing third-party runs on a survey page

Answering a survey loads no analytics, no fonts, no tag managers and no CDN scripts — nothing but this application. The anti-bot check is our own, in-page, and identifies nobody. A test fails the build if any third-party origin appears on a respondent page.

## Where the data lives, and who touches it

{{if .Region}}Hosted in {{.Region}}. These are every company involved:{{else}}These are every company involved in running this instance:{{end}}

| Processor | What for | What they see | Where |
|---|---|---|---|
{{- if .GoogleCloud}}
| Google Cloud | Hosting: the application, the database, backups and logs | Everything the service holds | europe-west4 |
{{- end}}
{{- if .Brevo}}
| Brevo | Sending sign-in links and survey invitations | Email addresses of account holders and invited participants | EU (France) |
{{- end}}
{{- if eq .AI "vertex"}}
| Google Vertex AI | Transcribing spoken answers, drafting questions, summaries and translations | Audio in transit (never stored), question and answer text | {{if eq .VertexLocation "eu"}}EU (Google Cloud multi-region: processed only in EU member states){{else if eq .VertexLocation "us"}}United States (Google Cloud multi-region){{else}}{{.VertexLocation}}{{end}} |
{{- end}}
{{- if eq .AI "openai"}}
| Self-configured AI backend | Transcription, drafting, summaries and translations | Audio in transit (never stored), question and answer text | Wherever this instance's operator points it |
{{- end}}
{{- if .GoogleLogin}}
| Google Identity | Signing in, only for people who choose Google | Email address and Google account id | Global |
{{- end}}
{{- if .NoProcessors}}
| Nobody | This instance runs entirely on its operator's own infrastructure | — | — |
{{- end}}

Self-hosting Earful removes all of them: it runs against your own Postgres, your own SMTP server and, if you want AI features, your own model.

## What we can't promise

Our infrastructure is in the EU, but Google Cloud's parent company is American, and US law reaches American companies wherever their servers are. EU hosting reduces that risk; it does not remove it. We would rather say so than imply a guarantee we cannot give.

Deleted data is removed from live systems immediately and erased permanently within 30 days. Backups are kept for 30 days and are deliberately immutable — which means an erasure is fully effective within 30 days, not instantly. That is the standard trade against losing everything to ransomware, and we think it is the right one.

## You can leave

One button exports everything a workspace holds — every survey, version, question and response — as documented JSON plus CSVs. The format is published and versioned, and Earful itself is AGPL-3.0, so you can run the same software yourself and bring your data with you.

[Source code](https://github.com/TryEarful/earful) · [Export format](https://github.com/TryEarful/earful/blob/main/docs/export-format.md)

## Attribution

{{.GeoAttribution}} — [db-ip.com]({{.GeoAttributionURL}})

***

{{if .ContactEmail}}Questions, or a request about your own data: [{{.ContactEmail}}](mailto:{{.ContactEmail}}).{{else}}This instance has not published a contact address. Ask whoever sent you the survey — they decide what happens to your answers, and they can reach the people running this instance.{{end}}
