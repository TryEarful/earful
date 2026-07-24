# Earful on Google Cloud — OpenTofu

Everything Earful runs on in GCP is described here: four projects
(`earful-ops/stg/pro/backups-<sfx>`), Cloud Run + Cloud SQL per
environment, one shared Artifact Registry (production promotes the exact
digest staging smoke-tested), Cloud DNS for tryearful.com, keyless GitHub
deploys (Workload Identity Federation), the alert set, and the immutable
backup pipeline (ADR-0008).

Layout:

```
bootstrap/   run once: projects, APIs, state bucket, WIF, DNS zone,
             shared registry, billing budgets
modules/     cloudsql, run-service, secrets, deploy-sa, domain,
             monitoring, backups (+ its tofu test)
envs/stg/    staging   (db-f1-micro, console email, e2e smoke target)
envs/pro/    production (db-g1-small, PITR, deletion protection,
             Brevo, daily immutable exports)
```

Day-to-day rule: **the pipeline owns images, tofu owns everything else.**
Both Cloud Run resources `ignore_changes` on image, so a later
`tofu apply` never rolls the app back to the seed image.

## Operator sequence (from zero)

Interactive prerequisites (browser windows):

```
gcloud auth login
gcloud auth application-default login
gcloud billing accounts list       # note ID; currency should be EUR
```

**1. Bootstrap** (local state on the first apply):

```
cd deploy/opentofu/bootstrap
tofu init
tofu apply   # vars: billing_account, suffix (2-8 lowercase alnum)
```

Note the outputs: nameservers, WIF provider, registry, state bucket.
If the identity lacks billing-budget IAM, re-apply with
`-var create_budgets=false` and configure €50/€80/€100 (+€200 cap)
alerts by hand.

**2. Migrate bootstrap's own state** into the bucket it just made:
uncomment `bootstrap/backend.tf`, then
`tofu init -migrate-state -backend-config="bucket=earful-tofu-state-<sfx>"`
and delete the local `terraform.tfstate*`.

**3. Seed image** (once; the pipeline owns every later image):

```
gcloud auth configure-docker europe-west4-docker.pkg.dev
docker build --platform linux/amd64 \
  -t europe-west4-docker.pkg.dev/earful-ops-<sfx>/earful/earful:seed .
docker push europe-west4-docker.pkg.dev/earful-ops-<sfx>/earful/earful:seed
```

**4. Staging:**

```
cd ../envs/stg
tofu init -backend-config="bucket=earful-tofu-state-<sfx>" -backend-config="prefix=stg"
tofu apply -var state_bucket=earful-tofu-state-<sfx>
# then, from the outputs:
gcloud run jobs execute earful-migrate --project earful-stg-<sfx> --region europe-west4 --wait
curl -f "$(tofu output -raw service_url)/health"    # M1-T1 met (/healthz is GFE-reserved on public URLs)
```

**5. Production:** same dance in `envs/pro` (prefix=pro). Boots with
`email_sender=console` — flip to Brevo in step 9.

**6.–7. Pipeline:** set the repo variables listed at the top of
`.github/workflows/deploy.yml` (`gh variable set NAME -b VALUE`), push
main, watch the run: build → migrate → deploy stg → full e2e smoke
(magic links read back from Cloud Logging — staging's console sender is
load-bearing, never point staging at a real ESP). Tag `vX.Y.Z` to
promote that digest to pro.

**8. Domain cutover** (the zone already replicates every live record —
GitHub Pages apex/www, Workspace MX/DKIM/TXT — see `bootstrap/dns.tf`):

```
# pre-cutover gate: EVERY record must answer identically from the new NS
for ns in $(cd bootstrap && tofu output -json dns_name_servers | jq -r '.[]'); do
  for q in "tryearful.com A" "tryearful.com AAAA" "tryearful.com MX" \
           "tryearful.com TXT" "www.tryearful.com CNAME" \
           "google._domainkey.tryearful.com TXT"; do
    diff <(dig +short @$ns $q | sort) <(dig +short $q | sort) || echo "MISMATCH: $q @ $ns"
  done
done
```

Only when clean: flip nameservers at Gandi → wait for propagation →
re-apply stg with `-var custom_domain=stg.tryearful.com` and pro with
`-var custom_domain=app.tryearful.com` (creates mappings + CNAMEs, flips
BASE_URL and the uptime checks). Managed certs take 15 min–24 h.
Rollback at any point = revert nameservers at Gandi (the old LiveDNS
zone stays intact there).

**9. Brevo** (production email):

```
printf '%s' "$BREVO_API_KEY" | gcloud secrets versions add BREVO_API_KEY \
  --project earful-pro-<sfx> --data-file=-
```

Add Brevo's DKIM/SPF/DMARC values for mail.tryearful.com to bootstrap's
`mail_dns_records` var → apply bootstrap → re-apply pro with
`-var email_sender=brevo` → configure Brevo's webhook to the
`email_webhook_path` output → send a real message and check DMARC.

**10. Drills** — see `docs/runbook.md`: DB-kill uptime alert, budget
test-fire, PITR clone restore, first export + restore from it, then and
only then `-var lock_retention=true` (IRREVERSIBLE) and the
owner-cannot-delete test.

## Notes

- `*.tfvars` are gitignored on purpose (billing account, suffix);
  `.terraform.lock.hcl` files are committed.
- Cloud SQL keeps a public IP with **zero** authorized networks: the
  only path in is the IAM-gated connector socket. Private-IP-only would
  add a paid VPC connector for nothing at this size.
- `max_instances = 1` is deliberate (in-memory rate limiters + budget);
  revisit with a shared-state limiter if traffic outgrows it.
- CI runs `tofu fmt -check`, `tofu validate` on all roots, and
  `modules/backups` contract tests (`tofu test`) on every push.
