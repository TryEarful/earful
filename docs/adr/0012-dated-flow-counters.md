# Flow counters carry a date; audience counters never do

Amends ADR-0009. The three survey flow counters — opened, submitted, and the question where a submitted response's answers stopped — are recorded per UTC day in a second counter table, `survey_stats_daily`, so a creator can read a survey over a date range and see a trend. The three audience counters — browser family, device class, country — stay as undated totals in `survey_stats`, and no date filter is ever applied to them. Everything ADR-0009 requires of a counter holds for the dated table: no join path to a response, the same build-time unlinkability tests, purged with the survey. "Where answers stop" is keyed by Question Identity in the dated table rather than by position, so a question inserted in a later version does not shift earlier counts onto the wrong row.

## Why the split

The n < 5 suppression rule protects a single query. A date range is a second query. A creator who can ask for 1–3 July and then 1–2 July subtracts the two and has 3 July's countries, browsers and devices, both answers having cleared the threshold; on a small anonymous survey one day is one person. Opened and submitted counts per day reveal nothing of the kind: the responses behind them are already listed one by one, with timestamps, on the results page.

## Considered Options

- Date every metric and suppress on the filtered sum: matches the mockup that prompted this, including a device filter, and hands out the subtraction attack above. Rejected.
- Date every metric, but allow audience filtering only on invited surveys, where each response is already tied to an email: defensible, but a branch in every query and template for a filter nobody asked for. Rejected for now; reopening this ADR is the way to add it.
- Add a day column to `survey_stats` itself: one table, but the existing rows need a sentinel day and the audience rows would carry a meaningless one forever. A second table keeps the old shape untouched.
- A "started answering" beacon from respondent pages, to separate views from starts: within ADR-0006's letter (first-party, cookieless), but the first time a respondent page would report behaviour rather than a result. Not done; M7-T4's stance on quiet respondent pages stands, and this is the ticket to write if the completion rate ever reads as too pessimistic.

## Consequences

- Counters recorded before this change have no date. They are added into all-time totals, shown with a note, and left out of any narrower range, shown with a different note. Nothing is backfilled because nothing can be.
- The blessed list in ADR-0009 is unchanged. The dated table's CHECK admits exactly `start`, `completion` and `reached`, so an audience metric written there fails loudly.
- The workspace export format moves to version 2 with a `stats_daily` array, documented in `docs/export-format.md`.
- The privacy notice describes per-day counting of opens and submissions.
