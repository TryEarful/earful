# Founder metrics — what the numbers mean

`/admin/metrics` (super admin only, M9-T7). A number without a
definition is an argument waiting to happen, so here are the
definitions.

The whole point of doing this ourselves is that respondent pages stay
quiet: there is no analytics script, no beacon, and no third party
involved in producing any of it (ADR-0006). Everything below is a count
of the product's own objects.

## Totals

| Metric | Definition | Watch for |
|---|---|---|
| **Accounts** | Users not soft-deleted. | Growth; also the denominator for activation. |
| **Workspaces** | Workspaces not soft-deleted. One per account today (MVP is sole-member), so this tracks Accounts until member invites ship. |
| **Surveys** | Surveys not soft-deleted, drafts included. | A gap between this and Published surveys means people start and stop. |
| **Published surveys** | Surveys with at least one Survey Version. | The real activation metric: a draft nobody published helped nobody. |
| **Responses** | Responses not soft-deleted, across every survey. | The product's actual output. |
| **Participants invited** | Participant rows, i.e. addresses imported into invited surveys. | Not "emails sent": sending is drip-capped and may lag. |
| **Completion rate** | `completion ÷ start` summed over the unlinked survey counters. | See the caveats below — it is an underestimate. |

## Series (last 30 days)

- **Signups** — accounts created per day, by `users.created_at`.
- **Responses** — responses submitted per day, by `responses.submitted_at`.
- **AI spend** — `ai_usage` rows summed per accounting day (UTC).

## Caveats worth remembering before quoting a number

**Completion rate is an underestimate, deliberately.** "Starts" counts
loads of a respondent page, and a page load is not a person: one
respondent who reloads twice counts twice. The counter is rate-limited
per network so a crawler cannot wreck it, which means a genuinely busy
survey behind one corporate NAT may under-count starts instead. Read it
as a trend, never as a percentage to put in a deck.

**AI spend is an estimate, not an invoice.** Tokens are estimated from
characters (~4 per token) and audio from duration (~32 tokens/second),
and the euro figure applies a configured per-1k rate. It errs high,
which is the right direction for a budget guard and the wrong direction
for accounting. The authority on cost is the cloud bill; this is the
number the breaker acts on.

**Nothing here is per-respondent.** There is no funnel by person, no
cohort, no session replay — not because it would be hard, but because
the product's promise to respondents is that it does not do that. If a
future question needs per-person data to answer, the answer is that we
do not answer it.

**Soft-deleted data disappears from these counts immediately** and is
erased 30 days later. A sudden drop is usually somebody deleting a
survey, not a bug.
