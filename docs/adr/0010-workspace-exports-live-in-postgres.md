# Workspace export archives live in Postgres, not object storage

A workspace export is built in the background and stored as bytes in an
`export_jobs` row, downloadable through the application for 24 hours.
There is no bucket, no signed URL and no lifecycle rule.

## Considered Options

- **GCS with a signed URL**: the cloud-native answer, and unbounded in
  size. But it makes the export a *hosted* feature: a self-hoster running
  `docker compose up` would either need to configure object storage or
  find the button broken, and "you can leave with your data" is precisely
  the promise that must not be weaker on the self-hosted side. It also
  adds a bucket, IAM, a lifecycle rule and a second retention story to
  the trust page.
- **Stream the archive synchronously from the download request**: no
  storage at all, but a large workspace would hold a request open past
  Cloud Run's timeout, with no way to retry a failure and nothing to show
  while it builds.
- **Postgres bytea, built asynchronously (chosen).**

## Consequences

- One code path everywhere. The SaaS and a laptop produce the same
  archive with the same code, which is the only way the format stays a
  real contract (docs/export-format.md).
- Archives are capped at 64 MB, and a workspace above that gets a message
  saying so with per-survey CSV as the fallback. This is the real cost of
  the decision and it is stated rather than discovered.
- Backups grow by the size of live exports. The 24-hour expiry and the
  purge job keep that bounded; expired archives are cleared, not kept.
- The download link is the job id and needs a session in the owning
  workspace. It is not a shareable capability — acceptable while a
  workspace is single-member, and worth revisiting alongside member
  invites.
