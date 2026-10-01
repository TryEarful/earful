# Responses leave on a schedule, signed, for an address the workspace chose

Status: Proposed

A workspace may name a Destination: an HTTPS address to which Earful
sends each new Response, signed, on a schedule. A scheduled job,
`earful sync`, run the way `earful purge` is run, reads the Responses
submitted since the Destination's cursor and posts them one at a time.
The cursor moves only past a Response the address accepted. The payload
is a strict subset of what the workspace export already carries, built
from the live rows at the moment of sending and never stored.

The first kind of Destination is a signed webhook, which is what Zapier,
Make, n8n and a creator's own server all accept. Destinations that need
Earful to hold an account at another company, such as OneDrive through
Microsoft Graph, are a later kind under the rules below, and each one is
named on the trust page before it ships.

## Context

Creators collect answers in Earful and work on them elsewhere. Today the
only way out is by hand: the per-survey CSV (story 58) or the workspace
archive (story 59, ADR-0010). Issue #24 asks for a periodic export to a
system the creator chooses. `PLAN.md` and `SPEC.md` list CRM integrations
as out of scope; this narrows that line rather than removing it.

What the product has, and what constrains the design:

- **Nothing outbound on a creator's behalf.** Every outbound call today
  goes to a party the operator chose and the trust page names: Vertex
  (ADR-0013), the ESP (ADR-0005), Google sign in. The only webhook is
  inbound (`emailWebhook` in `internal/http/participants.go`), with its
  secret in the path. A Destination is the first address supplied by a
  user, which makes the server a client of anywhere.
- **The server can reach things it should not.** Cloud Run has no VPC
  connector here, so the instance metadata server at `169.254.169.254`
  (and `metadata.google.internal`) is reachable from the container and
  hands out the service account's token. A self-hosted instance sits on
  a compose network beside Postgres and the mail catcher. A request to a
  user-supplied URL that is not checked is a request into those.
- **Respondent pages are untouched.** The CSP in
  `internal/http/security.go` (`connect-src 'self'`, ADR-0006) governs
  browsers. Delivery runs on the server, out of any respondent's request,
  so the policy does not change and no respondent page learns that a
  Destination exists.
- **Every feature works without JavaScript.** Creating, testing and
  removing a Destination are forms.
- **Anonymity is strong and immutable (ADR-0003).** An anonymous Response
  carries no identifying data, and nothing may be added on the way out.
- **The export format is a contract** (`docs/export-format.md`). What a
  Destination receives is a second published shape and is versioned the
  same way.
- **Erasure is single-row (ADR-0001)** and the purge keeps "deleted in 30
  days" true (`internal/purge`). A stored copy of a payload would be a
  second copy of every answer for erasure to chase.
- **EU residency (ADR-0011, ADR-0013)** describes where Earful processes
  data. A Destination is wherever its owner says, usually outside the EU
  (Zapier is American), and Earful cannot know.
- **One binary, one code path (ADR-0007, ADR-0010).** The nightly purge is
  a Cloud Run Job started by Cloud Scheduler
  (`deploy/opentofu/modules/run-service/main.tf`); a self-hoster runs the
  same subcommand from cron. The serving container's CPU is throttled
  between requests, so an in-process ticker would not run reliably on
  Cloud Run.

## Decision

**A scheduled job, not a call on submit.** `earful sync` runs on a
schedule (hourly in the SaaS) and delivers for every Destination that is
due under its cadence (hourly or daily). Delivery never happens in the
respondent's request: a slow or failing third party cannot delay or fail
a submission, a respondent's request never causes a request elsewhere,
and a burst of answers cannot be turned into a burst of outbound calls.
The same per-Destination delivery runs when a creator presses "Send now",
synchronously, so it can be used and tested without the schedule.

**A signed webhook first.** Each Response is one `POST` of JSON, signed
under the Standard Webhooks scheme: headers `webhook-id`,
`webhook-timestamp` and `webhook-signature`, the signature being
`v1,` and the base64 HMAC-SHA256 of `id.timestamp.body` under a 32-byte
secret Earful generates. A published scheme means a receiver can verify
with an existing library rather than a description. One request per
Response, rather than a batch, is what Zapier and Make each turn into one
run of a workflow; a batch would need a splitting step on their side.

**What is sent.** The payload is a subset of the workspace export and
gains nothing it lacks:

```jsonc
{
  "type": "response.submitted",
  "format_version": 1,
  "workspace": { "id": "uuid" },
  "survey": { "id": "uuid", "title": "…", "is_anonymous": true },
  "response": {
    "id": "uuid",
    "version": 3,
    "submitted_at": "2026-07-02T18:22:00Z",
    "duration_secs": 143,                  // omitted when unknown
    "participant_email": "…",              // invited surveys, and only when the Destination asks
    "answers": [
      {
        "identity_id": "uuid",
        "question": "What stood out?",     // the wording that Response was served
        "type": "long_text",
        "value": { "text": "…" },          // the export's answer object
        "display": "…"                     // the CSV cell, for mapping in Zapier
      }
    ]
  }
}
```

Never sent: survey counters (ADR-0009, ADR-0012), Insight Summaries,
answer translations, anything from the abuse log, anything about the
respondent's request. An anonymous survey's payload has no participant
field because there is none to read. On an invited survey the
participant's email is sent only when the Destination was created with
that box ticked, off by default, since it is personal data leaving for a
third party and the creator should decide that once, in words.

**Incremental, at least once.** A Destination holds a cursor
`(submitted_at, response_id)`. A run reads, in that order, live
Responses of live surveys in the workspace past the cursor and older
than a settling margin (five minutes), so a submission whose transaction
began before the cursor moved is not skipped. Each 2xx moves the cursor
past that Response; the first failure ends the run for that Destination,
so order holds and nothing is skipped. A receiver can see a Response
twice (a timeout after it accepted) and deduplicates on `webhook-id`,
which is the Response's id. A run sends at most a fixed number of
Responses per Destination (500), so a first run on a large workspace
is spread over several.

**Failures.** Timeouts are 10 seconds to connect and 20 to answer. A
failed run sets `next_attempt_at` with exponential backoff (one hour,
doubling, at most a day). After 72 hours of consecutive failures, or at
once on `410 Gone`, the Destination is disabled with a reason, and the
workspace's members receive one email. A disabled Destination keeps its
cursor, so turning it on again resumes where it stopped. Earful records
attempts as counts and a status class (time, outcome, HTTP status,
Responses sent), never a body, and keeps them 30 days.

**The address is checked every time it is used.** Only `https`, only port
443, no credentials in the URL, a host name rather than a literal
address. The check runs when the form is saved and again at connection
time, inside the dialer (`net.Dialer.Control`), against the address being
dialled, so a name that later resolves somewhere else (DNS rebinding) is
refused at the moment it matters. Refused: loopback, RFC 1918 and
unique-local, link-local (which holds the metadata server), carrier-grade
NAT `100.64.0.0/10`, `0.0.0.0/8`, multicast, broadcast, documentation and
reserved ranges, and the IPv4-mapped and NAT64 forms of each. Redirects
are not followed, and a 3xx is a failure. Proxy settings from the
environment are ignored. TLS is verified normally. At most 64 KB of the
answer is read and then discarded.

**Secrets are encrypted at rest.** The signing secret is needed in clear
to sign, so it cannot be hashed the way session and invite tokens are.
The Destination URL is a credential too: a Zapier catch hook accepts
anything posted to it. Both are stored sealed with AES-256-GCM under
`DELIVERY_KEY`, a key held in Secret Manager in the SaaS and in the
environment of a self-hosted instance, so that the daily database export
(ADR-0008) and a leaked backup carry neither. Without the key the feature
is absent, the way AI features are absent without a provider. The page
shows the host only; the secret is shown once, at creation, and can be
replaced. Request logs never carry the URL.

**Abuse.** At most five Destinations per workspace, and at most ten test
sends per workspace per hour. The volume of real deliveries is bounded by
the Responses the workspace receives. Requests carry
`User-Agent: Earful-Delivery/1 (+https://<instance>/help/destinations)`.

**Lifecycle.** A Destination removed by its owner is deleted at once with
its attempts; it is configuration, and there is nothing to restore. A
soft-deleted survey or workspace stops delivering immediately, since the
job reads only live rows. The purge deletes the Destinations and attempts
of a workspace past its 30 days, and attempts older than 30 days
everywhere. Destinations are not part of the workspace export: they are
configuration holding secrets, and the format stays at version 2.

**Later kinds.** A kind that needs Earful to hold credentials at another
company (OneDrive, Google Drive, a CRM) writes the same subset of data,
and before it ships: its own amendment to this ADR, the company named on
`/trust` for instances that configure it (`{{if .OneDrive}}`), and the
data-processing terms read and recorded as ADR-0013 did for Vertex.
OneDrive would hold a Microsoft Graph refresh token sealed like the
secret above, with the narrowest scope (`Files.ReadWrite.AppFolder`), and
would replace one CSV per survey in the app's folder on each run, written
by `writeResultsCSV` in `internal/http/export_csv.go`.

## Considered Options

- **Deliver from the submit handler.** Immediate, and no job. But it puts
  a third party's latency and failures inside a respondent's request,
  needs a retry store anyway for the failures, and on Cloud Run a
  goroutine left running after the response is starved of CPU.
- **A periodic file instead of a webhook** (the workspace archive or the
  CSVs, pushed somewhere). Matches the issue's words, but no generic
  receiver takes a file by push without an account at a storage company,
  and re-sending everything each day multiplies traffic and the data in
  flight. Kept as the shape of the OneDrive kind, where the file is
  replaced in place.
- **Pull instead of push**: an API with a token, which Zapier polls. No
  egress at all and no address to check. But it needs API tokens, scopes
  and rate limits as a product of their own, and Zapier's polling trigger
  is a private app per user or a published integration Earful would have
  to maintain. Worth reconsidering if a public API is built for other
  reasons.
- **A delivery ledger** (a row per Destination per Response) instead of a
  cursor. Exact, and tolerant of any commit order, but it grows with every
  Response times every Destination and becomes a second table that names
  Responses for the purge to chase. The cursor with a settling margin
  costs one row per Destination.
- **Storing payloads in an outbox.** Retries replay exactly what was
  first built. But each payload is a copy of answers at rest, so a
  Response deleted or erased after queueing would still leave. Building
  at send time means a deletion before delivery is honoured.
- **Native Zapier and Make apps.** Better discovery in their catalogues,
  but each is a listing to maintain and review on another company's
  terms, and both already accept a signed webhook.
- **Allowing private addresses** for self-hosters with n8n on the same
  network. Useful, and the exact capability an attacker wants. Left out;
  an operator setting to allow named private ranges is an open question,
  not a default.

## Consequences

- **Amends ADR-0003**, by adding a consequence: anything that leaves an
  instance on a creator's instruction is a subset of the workspace
  export, and a new field in a delivered payload is a change to the
  documented format, reviewed as such.
- **Amends ADR-0007**: a second scheduled Cloud Run Job (`earful sync`)
  beside the purge, with its own Scheduler trigger, invoker binding and
  failure alert, its image moved by the deploy workflow as the purge's is.
- **Bounds ADR-0011 and ADR-0013.** Their promise is about where Earful
  processes data and is unchanged. The trust page and the Destination
  form say plainly that a Destination receives answers wherever its
  owner put it, under the owner's own terms with that recipient, and
  that Earful neither knows nor promises where that is. For respondent
  data the workspace is the controller; a webhook Destination is the
  controller's recipient, not Earful's sub-processor.
- **`docs/export-format.md`** gains a section, "Delivered responses,
  format 1", and is the contract for receivers as it is for importers.
- **Deletion does not travel.** A Response deleted in Earful after it was
  delivered stays at its Destination. The form and the help page say so;
  a `response.deleted` event is a possible later addition.
- **A guard joins the five in `docs/testing.md`**: the delivery package
  makes outbound requests only through the checked client, and a test
  fails the build if it constructs another.
- **Backups** carry Destinations sealed, so restoring one without
  `DELIVERY_KEY` restores Destinations that cannot send, which the page
  shows as needing their secret and URL entered again.
- **Cost** is one short job an hour and the egress of the answers
  themselves, within Appendix C's line for egress and Scheduler.
