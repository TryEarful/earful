# Proposal: responses delivered to a chosen destination (issue #24)

Design: [ADR-0022](../adr/0022-scheduled-delivery-to-a-chosen-destination.md),
status Proposed. This file is the build plan that follows from it, in
slices that each ship on their own and leave `make check`,
`make e2e-smoke` and the gallery green. Nothing here is built yet.

Vocabulary used below, to be added to `CONTEXT.md` with slice 1:
**Destination** (an address a workspace's Responses are sent to),
**Delivery** (one attempt to send one Response to one Destination),
**Cursor** (the last Response a Destination accepted).

## Slice 1: a webhook Destination, sent by hand (L)

What a creator gets: on the account page, a "Destinations" card. They add
an HTTPS address and a name, see the signing secret once, press "Send a
test" to see a sample arrive in Zapier or Make, and press "Send now" to
deliver every Response submitted since the Destination was created. The
schedule comes in slice 2; this slice is already a complete manual
integration.

### Migration

`db/migrations/000NN_delivery_destinations.sql`, with NN the next free
number when the slice is built:

```sql
CREATE TABLE delivery_destinations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        uuid NOT NULL REFERENCES workspaces (id),
    kind                text NOT NULL DEFAULT 'webhook',
    label               text NOT NULL,
    -- The URL is a credential (a catch hook accepts anything posted to
    -- it) and the secret must be readable to sign: both sealed under
    -- DELIVERY_KEY, so a database export carries neither (ADR-0022).
    url_sealed          bytea NOT NULL,
    url_host            text NOT NULL,          -- shown on the page; the URL never is
    secret_sealed       bytea NOT NULL,
    include_participant_email boolean NOT NULL DEFAULT false,
    cadence             text NOT NULL DEFAULT 'daily',
    cursor_submitted_at timestamptz NOT NULL,   -- creation time: "from now on"
    cursor_response_id  uuid,                   -- tie breaker; NULL before the first delivery
    status              text NOT NULL DEFAULT 'active',
    disabled_reason     text,
    failing_since       timestamptz,
    next_attempt_at     timestamptz,
    last_success_at     timestamptz,
    created_by          uuid REFERENCES users (id),
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT delivery_destinations_kind_known CHECK (kind IN ('webhook')),
    CONSTRAINT delivery_destinations_cadence_known CHECK (cadence IN ('hourly', 'daily')),
    CONSTRAINT delivery_destinations_status_known CHECK (status IN ('active', 'disabled'))
);
CREATE INDEX delivery_destinations_workspace_idx ON delivery_destinations (workspace_id);
CREATE INDEX delivery_destinations_due_idx ON delivery_destinations (next_attempt_at) WHERE status = 'active';

-- Counts and a status class only. Never a body: a stored payload would
-- be a copy of answers for erasure to chase (ADR-0001, ADR-0022).
CREATE TABLE delivery_attempts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    destination_id  uuid NOT NULL REFERENCES delivery_destinations (id) ON DELETE CASCADE,
    at              timestamptz NOT NULL DEFAULT now(),
    trigger         text NOT NULL,              -- 'test' | 'manual' | 'schedule'
    outcome         text NOT NULL,              -- 'delivered' | 'nothing_new' | 'failed' | 'refused'
    http_status     integer,
    error_class     text,                       -- 'timeout' | 'tls' | 'redirect' | 'address_refused' | 'status'
    responses_sent  integer NOT NULL DEFAULT 0
);
CREATE INDEX delivery_attempts_destination_idx ON delivery_attempts (destination_id, at DESC);
```

No foreign key from either table to `responses`: the cursor is a
position, not a reference, so the purge never has to visit it to erase a
Response.

### Code

| Path | What |
|---|---|
| `internal/delivery/guard.go` | `Client(opts)`: an `http.Client` whose dialer checks every address it dials (`net.Dialer.Control`), no redirects (`CheckRedirect` returns `http.ErrUseLastResponse`), `Proxy: nil`, timeouts 10 s connect and 20 s overall, a 64 KB read cap. `CheckURL(raw)`: https, port 443, no userinfo, not an IP literal. Blocked ranges listed once, as `netip.Prefix` values, including the IPv4-mapped and `64:ff9b::/96` forms. |
| `internal/delivery/sign.go` | Standard Webhooks signing: `webhook-id`, `webhook-timestamp`, `webhook-signature: v1,<base64 HMAC-SHA256(id.ts.body)>`; secrets rendered `whsec_<base64>`. |
| `internal/delivery/payload.go` | The format 1 payload (ADR-0022), built from `export.Answer` and `domain.AnswerValue.Display` so the value and the CSV cell are the same as in the export. |
| `internal/delivery/seal.go` | AES-256-GCM seal and open under `DELIVERY_KEY`, with the destination id as additional data so a sealed value cannot be moved to another row. |
| `internal/delivery/deliver.go` | `DeliverOne(ctx, deps, destinationID, trigger)`: reads up to 500 Responses past the cursor and older than five minutes, posts each, moves the cursor on each 2xx, stops at the first failure, writes one attempt row. Used by "Send now" here and by the job in slice 2. |
| `db/queries/delivery.sql` | Create, list by workspace, get (workspace scoped), delete, move cursor, record attempt, `ListResponsesAfterCursor(workspace_id, at, id, settle_before, limit)` joining live surveys and live responses only. |
| `internal/config/config.go` | `DELIVERY_KEY` (32 bytes, base64). Empty means the card is not shown, as with AI. |
| `internal/http/destinations.go` | `GET /account/destinations`, `POST /account/destinations` (create), `POST /account/destinations/{id}/test`, `POST /account/destinations/{id}/send`, `POST /account/destinations/{id}/secret` (replace), `POST /account/destinations/{id}/delete`. All forms, CSRF checked, workspace scoped, redirect with a notice. |
| `internal/http/routes.go` | The routes above, beside `/account/export`. |
| `internal/logging` | `ScrubURL` gains nothing: the URL is never in a request path or query. A test asserts no log line carries it. |
| `web/templates/destinations.templ` | The card and the add form (below). |
| `web/text/active.en.toml`, `active.es.toml` | Every label, notice, error and the disclosure sentence, under `destinations.`. |
| `web/pages/help/destinations.en.md`, `.es.md` | What is sent, the payload, how to verify a signature (a short example), what deletion does not do, and that the receiver's location is the creator's choice. |
| `web/pages/trust.en.md`, `.es.md` | One paragraph, shown when the instance has `DELIVERY_KEY`: answers can be sent where a workspace chooses, and from there they are under that workspace's arrangements, not Earful's. |
| `docs/export-format.md` | "Delivered responses, format 1": the payload, the headers, the at-least-once rule, the dedup key. |
| `internal/purge/purge.go` | Steps `delivery_attempts_of_deleted_workspaces` (via destinations) and `delivery_destinations_of_deleted_workspaces`, before `deleted_workspaces`; `old_delivery_attempts` (30 days). |
| `docs/testing.md` | The guard below added to "Guards that fail the build"; the receiver fake added to "Fakes at the true external boundaries". |

### The page

On `/account`, a card "Destinations" listing each one by name and host,
its last delivery, and its state. One filled button on the page stays
the account page's own; "Add a destination" is outlined. The add form
asks for a name, the address, and (on workspaces with invited surveys)
whether to include participants' email addresses, unticked, with the
sentence that the address will receive answers wherever it is hosted.
After saving, the page shows the secret once in a read-only field with
a copy button (the copy is a script enhancement; the text is selectable
without it).

### Tests at the application edge

All in `internal/http/destinations_test.go`, through `apptest`, with a
receiver that is an `httptest.NewTLSServer`. Its address is loopback, so
`apptest.Options{DeliveryAllow: []netip.Prefix{...}}` admits exactly that
server's address and nothing else; production code has no such option
outside configuration that is refused at boot outside tests.

- A creator adds a Destination, presses "Send a test", and the receiver
  gets one signed request whose signature verifies with the shown secret.
- Two Responses submitted through `/s/`, the clock advanced past the
  settling margin, "Send now": the receiver gets both, in order, and a
  second "Send now" sends nothing.
- An anonymous survey's payload has no participant field; an invited
  survey's has the email only when the box was ticked.
- The receiver answers 500 to the second of three: one is delivered, the
  next "Send now" starts at the second.
- A soft-deleted Response or survey before delivery is never sent.
- `http://`, an IP literal, a name resolving to `127.0.0.1`, to
  `169.254.169.254` and to `10.0.0.1` are refused at save time with a
  message, and a name that resolves to a public address at save time and
  a private one at send time is refused at send time (a resolver seam in
  the guard's options, as the clock is).
- A 302 from the receiver is a failure and the redirect target receives
  nothing.
- Another workspace's Destination is a 404 on every route.
- The page never contains the URL after saving, only the host.
- Purge (`internal/purge/purge_test.go`, isolated DB): a deleted
  workspace's Destinations and attempts go on day 31; attempts older than
  30 days go everywhere.

Guard, beside the five in `docs/testing.md`: a source scan fails the
build if `internal/delivery` constructs an `http.Client` or
`http.Transport` anywhere but `guard.go`.

### Gallery states

In `e2e/gallery/gallery.spec.ts`: account with no Destinations; the add
form; the add form refusing an address; a Destination just created,
showing its secret; the list with one active Destination after a
delivery; a test that failed, with its notice.

## Slice 2: on a schedule (M)

- `cmd/earful/sync.go`: `earful sync [--dry-run]`, the same shape as
  `runPurge` (`config.LoadJob`, a pool, counts only in the log). It
  claims due Destinations with `FOR UPDATE SKIP LOCKED` so overlapping
  runs do not double send, and calls `DeliverOne` for each.
- `cmd/earful/main.go`: the subcommand in the switch and the usage line.
- Cadence on the form: hourly or daily. Daily means at most once a day,
  at the first run after 24 hours since the last success.
- Failed runs set `next_attempt_at` with backoff: one hour, doubling, at
  most 24 hours.
- `deploy/opentofu/modules/run-service/main.tf`: `google_cloud_run_v2_job.sync`,
  `google_cloud_scheduler_job.sync` (`"23 * * * *"`, off the purge's
  minute) and its invoker binding, behind `enable_sync`, as the purge
  is; `DELIVERY_KEY` in the secrets module.
- `deploy/opentofu/modules/monitoring/main.tf`: an alert on a failed
  sync execution, behind the same flag.
- `.github/workflows/deploy.yml`: move the sync job's image with the
  purge's.
- `docs/runbook.md`: what the job does, how to run it by hand, what its
  alert means, rotating `DELIVERY_KEY` (re-seal with both keys loaded).
- `README.md`: `DELIVERY_KEY`, and a cron line for compose
  (`docker compose run --rm app sync`, hourly).
- Tests: a sync suite on `apptest.NewIsolatedDB(t, "sync")`, since the
  job is global as the purge is: due and not due Destinations, hourly
  and daily, backoff after a failure, two concurrent runs sending each
  Response once.
- Gallery: a Destination showing its next scheduled delivery.

## Slice 3: failing Destinations turn themselves off (S)

- After 72 hours of failures, or at once on `410 Gone`, status becomes
  `disabled` with a reason, and each workspace member gets one email
  (through `email.Sender`, worded in `web/text`). The job has no request
  to take a language from and accounts store none (ADR-0014), so the
  email is in English until they do.
- "Turn on again" on the card resumes from the kept cursor.
- Tests: failures past the window disable and send one email; 410
  disables at once; turning on again delivers what was waiting.
- Gallery: a disabled Destination with its reason.

## Slice 4: choosing what is sent (S)

- Pick surveys: all (the default) or a list, in a
  `delivery_destination_surveys` table, purged with either side.
- Starting point at creation: "from now on" (the default) or "everything
  so far", which sets the cursor to the epoch and lets the 500 per run
  cap spread the backfill.
- Tests and gallery states for both.

## Slice 5: OneDrive (L, separate approval)

Only after the owner accepts ADR-0022's later kinds section and the
terms are read and recorded. An Entra app registration, OAuth with
`Files.ReadWrite.AppFolder`, the refresh token sealed like the secret,
and on each run one CSV per survey replaced in the app folder, written by
`writeResultsCSV`. Large files go through an upload session. Microsoft is
named on `/trust` under `{{if .OneDrive}}`, and Appendix B of `PLAN.md`
gains a row for it. Until then, creators reach OneDrive through Zapier or
Make from slice 1, which the help page shows.

## Open questions for the owner

1. **Is Zapier the target, or OneDrive?** Slice 1 reaches both through
   Zapier or Make. If a native OneDrive connector is wanted regardless,
   slice 5 needs its own approval and a reading of Microsoft's terms.
2. **Participant emails.** Off by default with a per-Destination box, as
   proposed, or never sent at all?
3. **Starting point.** "From now on" only, or also "everything so far"
   (slice 4)?
4. **Workspace or survey level.** Proposed: a Destination belongs to the
   workspace and sends every survey, with a filter later. Should the
   first version be per survey instead?
5. **Cadence.** Hourly and daily, or should the SaaS job run more often
   (every 15 minutes) so delivery feels close to live?
6. **Private addresses for self-hosters.** Should an operator be able to
   allow named private ranges (n8n on the same network) through
   configuration?
7. **Deletion.** Is "deletion does not travel, and the page says so"
   acceptable, or is a `response.deleted` event wanted?
8. **Legal position.** The proposal treats a webhook Destination as the
   controller's own recipient, not an Earful sub-processor. Counsel
   (Appendix B, gap 19) should confirm before the trust page says so,
   and before any connector where Earful holds the account.
9. **Limits.** Five Destinations per workspace, ten tests an hour, 500
   Responses per Destination per run: right for the beta?
10. **Key management.** One `DELIVERY_KEY` per instance, sealed with
    AES-GCM, as proposed, or Cloud KMS in the SaaS at the cost of a
    second code path?
