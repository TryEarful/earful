# Security review (M9-T3)

Reviewed 2026-07-25, covering everything through M11. The AC is
"findings triaged to zero criticals"; this is that triage, plus the
mechanisms that keep it true as the code changes.

## What runs continuously

| Check | Where | What it catches |
|---|---|---|
| `govulncheck` | `make check`, every CI run | Known CVEs in the dependency graph, filtered to code actually reachable |
| `staticcheck` + `go vet` | `make check` | Bug patterns, unsafe conversions, dead error paths |
| Secret scanning | GitHub push protection + gitleaks in CI | A credential reaching a commit — the two catch different shapes |
| Renovate | `.github/renovate.json`, weekly | Dependency drift; vulnerability alerts are unscheduled |
| Cross-workspace denial suite | `internal/http/surveys_test.go` and per-feature tests | A new route forgetting workspace scoping |
| Metered-AI guard | `internal/http/ai_meter_guard_test.go` | An AI call that skips the budget breaker |
| Aggregate unlinkability | `internal/http/stats_test.go` | A query joining survey counters to responses (ADR-0009) |
| Audio non-persistence | `internal/voice/voice_test.go` | The audio package gaining any way to write bytes anywhere (ADR-0004) |
| No third-party origins | `internal/http/respond_test.go` | A respondent page gaining an external request (ADR-0006) |

Four of those are guards written because a rule that only lives in a
review gets broken by the next feature. Two of them have already caught
real regressions during this milestone (the meter guard caught a
per-batch quota check that could overshoot twentyfold; the axe gate
caught two accessibility defects).

## Findings

**None critical.** Two were found and fixed during the rollout of
2026-07-25; the rest are accepted risks or scheduled work, each with its
reasoning.

### Found and fixed

1. **Path-borne credentials reached the log sink.** `logging.ScrubURL`
   redacted query parameters only, and `RequestLogger` writes every
   request URL — so the ESP webhook secret (`/webhooks/email/{secret}`)
   and participant invite tokens (`/p/{token}`, where the token *is* the
   credential) were written verbatim at INFO on every request. Magic
   links were never affected because they use `?token=`, which is
   precisely why this survived review. Fixed by redacting the credential
   segment of the token-bearing routes; survey share links stay readable
   because they are public by design. Confirmed in the live log sink
   afterwards: `/exports/[REDACTED]`.
2. **Staging's wall could not pass a WebSocket.** Chrome does not send
   cached HTTP credentials on a handshake, so voice and streamed
   generation got a 401 on staging for anyone — the browser suite merely
   found it first. Fixed by issuing a cookie on the first authenticated
   request (HttpOnly, Secure, derived from the credential so it survives
   across Cloud Run instances, invalidated by rotation). Not a
   confidentiality bug — the wall never let anything through it
   shouldn't — but a wall that silently breaks one transport is a wall
   people route around.

### Accepted, documented

1. **CLOUD Act exposure.** EU hosting on Google Cloud does not put data
   beyond US jurisdiction. Mitigations (CMEK with external keys,
   sovereign cloud) are disproportionate at this scale. Stated plainly on
   `/trust` rather than glossed.
2. **Erasure is complete within 30 days, not instantly.** PITR (7 days)
   and the immutable exports (30 days) deliberately outlive a purge. The
   alternative — deletable backups — trades a stronger privacy claim for
   a much worse ransomware position. Documented on `/trust` and in the
   runbook.
3. **Anonymous surveys accept repeat submissions.** Anyone with the link
   can answer more than once; the anti-abuse layer raises the cost but
   cannot eliminate it without identifying respondents, which is the
   thing being protected. Stated to creators in the UI.
4. **Rate limiters are per-process.** With more than one Cloud Run
   instance, the effective limit multiplies by instance count. Acceptable
   while production runs a small instance count; a shared limiter is the
   fix if that changes.
5. **`AI_PROVIDER=scripted` invents content.** Refused at boot in
   production, so no real respondent can ever be served canned text
   presented as AI output. Staging may use it (changed 2026-07-25): it
   has no real respondents, and it needs a deterministic backend for the
   same reason CI does — specifically so a browser suite with no
   microphone is not sending synthesized audio to a real speech model.

### Scheduled

6. **Rate-limit soak against staging** (`tools/soak`). The limiter's
   logic is unit-tested and its per-request cost is trivial, but the
   AC asks for a load check against a deployed instance. The tool is
   written and documented. Blocked as of 2026-07-25: the staging project
   was suspended by Google mid-rollout (see the runbook), and the soak
   deliberately does not run against production — hammering the live
   respondent path to watch it refuse is not a thing to do to real
   respondents.
7. **Vertex terms in writing** — no-training, EU processing, abuse-log
   retention for the exact APIs used. Gap list #7; blocks nothing
   technically, and matters before the product is sold.
8. **Dependency count.** 9 direct modules, all either stdlib-adjacent or
   single-purpose. `cloud.google.com/go/compute/metadata` and
   `github.com/coder/websocket` were added this milestone; both were
   weighed against hand-rolling and both won on the "we would write the
   same thing, worse" test.

## Notes on the shape of the attack surface

- **Respondent pages carry no session and no CSRF token.** They are
  guarded by the stdlib cross-origin layer, an HMAC-signed render
  timestamp, a proof-of-work, honeypots and per-IP+survey rate buckets.
  A new respondent-facing POST must join that gauntlet, not sit beside
  it; `internal/http/respond.go` documents the order and the reasons.
- **WebSockets bypass the cross-origin layer** (a handshake is a GET), so
  `internal/ws` verifies the origin itself and refuses anything
  cross-origin. That is the single reason the package exists rather than
  being three helpers.
- **The purge is the only code allowed to DELETE** immutable rows, and it
  is allowed only through a transaction-local `earful.purging` setting
  the triggers check. Anything else that needs to delete should be asked
  hard questions.
- **Tokens are hashed at rest** — sessions, magic links, invites, beta
  codes. A database leak cannot be replayed into live access.
