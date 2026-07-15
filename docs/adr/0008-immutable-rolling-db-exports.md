# Daily immutable DB exports; rolling window via lifecycle, not delete permissions

As ransomware/malware protection, a daily Cloud Scheduler job triggers a Cloud SQL Admin API export of production into a GCS bucket in a separate backups project. The bucket carries a locked 30-day retention policy plus a lifecycle rule deleting objects older than 30 days. The export service account can only create objects. The rolling 30-day window therefore maintains itself, and no credential anywhere in the system — including the scheduler's and a project owner's — can delete a backup younger than 30 days.

## Considered Options

- Cron deletes old backups (original idea): whatever credential the cron holds, malware can hold too — deletion rights in the backup path defeat the purpose. Replaced by lifecycle + retention lock.
- Rely on Cloud SQL automated backups/PITR alone: those live with the instance and die with an attacker who gains app-project admin. Kept (M9-T1) but insufficient alone.

## Consequences

- The retention policy lock is irreversible by design — the 30-day floor cannot be shortened later, only the bucket abandoned for a new one.
- GDPR erasure is fully effective only after ≤30 days; documented in the privacy policy (gap list #5).
- Restore-from-export is a drilled runbook procedure, not a theory; export SA remains create-only.
