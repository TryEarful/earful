# SaaS runs on Cloud Run, not GKE (for now)

The original spec said Kubernetes. We deliberately chose Cloud Run for stg and pro: at MVP traffic, GKE's fixed floor (nodes 24/7, load balancer, Cloud NAT, control plane) eats €90–250/mo of a €200/mo total budget before a single Gemini token, while Cloud Run's request-based billing plus scale-to-zero staging leaves that headroom for the AI features that differentiate the product. Both environments are separate GCP projects; the 30-day purge cron is the same Go binary run locally, deployed as a Cloud Scheduler–triggered Cloud Run job.

## Consequences

- WebSocket connections cap at 60 minutes on Cloud Run; the client must auto-reconnect (trivial for token streaming).
- Everything ships as plain containers managed by opentofu, so moving to GKE later is a redeploy, not a rewrite. Revisit when sustained traffic keeps nodes busy, or a workload needs >60-min sockets or DaemonSet-style agents.
- Self-hosters are unaffected: docker compose (and optionally Helm) regardless of what the SaaS runs on.
