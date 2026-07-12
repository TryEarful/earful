# Pin responses to immutable survey versions — no copy-forward

Editing a published survey creates a new immutable Survey Version (append-only). A Response is stored once, pinned to the exact version the respondent was served, and is never copied to newer versions. Questions carry a stable Question Identity across versions; the results view aggregates responses across versions by that identity at read time, labelling wording changes per version.

## Considered Options

- **Copy replies forward on publish** (original idea): maximum redundancy, but misattributes answers to reworded questions, multiplies rows on every publish, and a GDPR erasure must chase every copy.
- **Pin, don't copy** (chosen): same read-time outcome — old replies visible in current results — with single-row erasure, no storage multiplication, and visible wording drift.

## Consequences

- The no-data-loss guarantee moves from "we duplicate data" to "versions and responses are immutable"; deletion happens only via the soft-delete + purge path.
- Results queries join across versions by Question Identity; per-version wording must be shown in the UI so drift is never hidden.
