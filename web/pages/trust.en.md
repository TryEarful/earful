---
title: How Earful treats your data
short_title: Trust
sections: cards
hash: sha256-b35ce64de0105f0700233e0d3c580034269058fcbd60aaa0b8cf466c5c4243e8
last_update: 2026-10-01
---

This page describes {{if .Instance}}{{.Instance}}{{else}}this instance{{end}}. Earful is open source (AGPL 3.0), so every claim here can be checked against the code that serves it.

## Your voice is never stored

You can answer by speaking. The audio is turned into text and discarded in the same request. It is never written to disk, to a database, to logs, or to any backup, in any environment. What is kept is the transcript you read and edited before submitting.

There is no playback feature, because there is nothing to play back. If that ever changed it would be a different product decision, announced here and asked of you first, not something that could happen quietly to audio we had kept in the meantime.

## Anonymous means anonymous

A survey's creator chooses at creation whether it is anonymous, and that choice can never be changed afterwards. The database refuses it, not just the application.

An anonymous response carries no email address, no IP address and no device details. Those columns do not exist anywhere near a response, so no query and no mistake can quietly fill them in. Adding them would take a deliberate change to the database, in public, in an open source repository.

Survey creators do see a few totals about their audience, never attached to any response:

- Browser family, device type and country, hidden for any group smaller than five people. Country is worked out on our own server from an offline database, and the IP address is discarded immediately.
- How many times the survey was opened and how many answers were submitted, per day. The audience totals are never split by day.

## Nothing from third parties runs on a survey page

Answering a survey loads no analytics, no fonts, no tag managers and no CDN scripts: nothing but this application. The bot check is our own, runs in the page, and identifies nobody. A test fails the build if any outside origin appears on a respondent page.

## The language of the interface

If you choose a language for the interface, your choice is kept in a cookie in your browser, named `interface_lang`, for a year. It is not set or read on a survey page: there the language is chosen in the address of the page and stored nowhere.

## The theme

If you choose a light or a dark theme on any page here, a survey included, your choice is kept for a year in a cookie in your browser named `theme`. It holds the word `light` or `dark` and nothing else, is set only when you choose, and is read only to draw the page. Choosing to follow your system removes it.

## Where the data lives, and who touches it

{{if .NoProcessors}}{{if .Region}}Hosted in {{.Region}}. {{end}}No outside company is involved: this instance runs entirely on its operator's own infrastructure.{{else}}{{if .Region}}Hosted in {{.Region}}. Every company involved:{{else}}Every company involved in running this instance:{{end}}{{end}}

{{- if .GoogleCloud}}
- **Google Cloud** hosts the application, the database, backups and logs, so it can see everything the service holds. Region `europe-west4`.
{{- end}}
{{- if .Brevo}}
- **Brevo** sends links to sign in and survey invitations. It sees the email addresses of account holders and invited participants. EU (France).
{{- end}}
{{- if eq .AI "vertex"}}
- **Google Vertex AI** transcribes spoken answers and drafts questions, summaries and translations. It sees audio in transit, never stored, the text of questions and answers, and any file a creator attaches when drafting questions, also never stored. {{if eq .VertexLocation "eu"}}EU, processed only in EU member states.{{else if eq .VertexLocation "us"}}United States.{{else}}Region `{{.VertexLocation}}`.{{end}}
{{- end}}
{{- if eq .AI "openai"}}
- **An AI service chosen by the operator** transcribes, drafts, summarises and translates. It sees audio in transit, never stored, the text of questions and answers, and the text of any file a creator attaches when drafting questions, also never stored, wherever this instance's operator points it.
{{- end}}
{{- if .VirusTotal}}
- **VirusTotal** is asked whether a file a creator attaches when drafting questions is known to be harmful. It is sent the file's `SHA-256` hash, never the file.
{{- end}}
{{- if .GoogleLogin}}
- **Google Identity** signs in the people who choose Google. It sees their email address and Google account id.
{{- end}}

{{if not .NoProcessors}}Hosting Earful yourself removes all of them: it runs against your own Postgres, your own SMTP server and, if you want AI features, your own model.{{end}}

## What we can't promise

{{if .GoogleCloud}}Our infrastructure is in the EU, but Google Cloud's parent company is American, and US law reaches American companies wherever their servers are. EU hosting reduces that risk; it does not remove it. We would rather say so than imply a guarantee we cannot give.{{end}}

Deleted data is removed from live systems immediately and erased permanently within 30 days. {{if .GoogleCloud}}Backups are kept for 30 days and are deliberately immutable, which means an erasure is fully effective within 30 days, not instantly. That is the standard trade against losing everything to ransomware, and we think it is the right one.{{else}}Backups are kept by this instance's operator, not by Earful: how long they are kept, and so when an erasure reaches them, is theirs to say.{{end}}

## You can leave

One button exports everything a workspace holds (every survey, version, question and response) as documented JSON plus CSVs. The format is published and versioned, and Earful itself is AGPL 3.0, so you can run the same software yourself and bring your data with you.

[Source code](https://github.com/TryEarful/earful) · [Export format](https://github.com/TryEarful/earful/blob/main/docs/export-format.md)

## Contact

{{if .ContactEmail}}Questions, or a request about your own data: [{{.ContactEmail}}](mailto:{{.ContactEmail}}).{{else}}This instance has not published a contact address. Ask whoever sent you the survey. They decide what happens to your answers, and they can reach the people running this instance.{{end}}

***

IP geolocation by {{.GeoSource}} · [db-ip.com]({{.GeoAttributionURL}})
