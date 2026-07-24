# Audience stats exist only as unlinked aggregates

Amends ADR-0003. Creators get survey-level statistics — starts, completions, completion rate, drop-off per question position, average duration — and audience aggregates: browser family, device class, and country. These live exclusively in counter tables with no join path to responses; country is derived at request time from an embedded GeoIP database resolved in-process (no lookup service, no new processor) and the IP is discarded immediately. Responses additionally carry a duration in seconds. The UI suppresses any aggregate bucket with fewer than 5 observations so small anonymous samples cannot be singled out.

## Considered Options

- Per-response metadata columns (browser/device/country on each response): simplest analytics, but makes small anonymous surveys deanonymizable and breaks ADR-0003's promise in spirit. Rejected.
- No metadata at all: keeps maximal purity but starves the product-improvement loop the collaborator's requirements rightly demand.

## Consequences

- The blessed list is exhaustive: browser family, device class, country, start/completion/drop-off counters, per-response duration — anything beyond it reopens this ADR.
- An automated unlinkability test must prove no application query can associate an aggregate with a response.
- The GeoIP database brings a licensing/attribution obligation and an update cadence (gap list); the privacy notice must describe the aggregate stats and geo derivation.
