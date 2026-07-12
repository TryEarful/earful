# No third-party scripts on respondent pages; ALTCHA in-app for challenges

Respondent-facing pages (survey answering, especially anonymous forms) load no third-party JavaScript, fonts, CDNs, analytics, or challenge widgets — everything is served from our own domain. Bot defence uses ALTCHA: an open-source proof-of-work widget served as our own static asset, with challenges generated and verified inside the Go app (altcha-org Go library). Layered with per-IP/per-survey rate limits, honeypot fields, noindex, and session-bound quotas plus a global daily budget breaker on LLM endpoints.

## Considered Options

- Cloudflare Turnstile: stronger against sophisticated bots, free — but injects a US processor's JS into the most privacy-sensitive page in the product. Rejected for brand/GDPR coherence; expect this suggestion to recur, and re-read this ADR when it does.
- Rate limits + honeypot only: acceptable week-1 posture, but retrofitting a challenge under active abuse is the worst time to do it.

## Consequences

- Any future analytics on respondent pages must be first-party and cookieless, or not exist.
- Challenge strength is deliberately traded down; the real cost-abuse defence lives in the LLM quota/budget layer.
