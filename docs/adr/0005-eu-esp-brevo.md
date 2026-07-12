# Email goes through an EU ESP (Brevo), behind a minimal interface

Google Cloud offers no first-party email sending (port 25 blocked; Google points at third parties), so an ESP is unavoidable. We chose Brevo (French, EU data centers): the MVP includes bulk participant invites — the hard deliverability case — and Brevo brings mature suppression/webhook/reputation tooling plus a free tier that covers early volumes. All sending sits behind a two-method Go interface (send, plus webhook ingestion) so the vendor is swappable; self-hosters configure any SMTP.

## Considered Options

- Scaleway TEM: purest EU story, cheaper, but a young transactional-only product with ramping quotas and thin deliverability guardrails for invite blasts. Documented fallback.
- Amazon SES / Postmark: cheaper / better DX respectively, but US-owned processors that weaken the sovereignty differentiator.

## Consequences

- SPF/DKIM/DMARC on a dedicated sending subdomain; bounce/complaint webhooks feed a suppression list; per-workspace invite caps and drip-sending from day 1.
- The durable decision is "EU-based ESP behind an interface" — the vendor itself is a config change.
