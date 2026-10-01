-- M7-T3: workspace export jobs.

-- name: CreateExportJob :one
INSERT INTO export_jobs (workspace_id, requested_by, status, created_at)
VALUES ($1, $2, 'pending', $3)
RETURNING id, workspace_id, status, size_bytes, error, created_at, finished_at, expires_at;

-- name: ClaimExportJob :one
-- Only one worker may build a job, whichever instance gets there first.
UPDATE export_jobs SET status = 'running'
WHERE id = $1 AND status = 'pending'
RETURNING id;

-- name: FinishExportJob :exec
UPDATE export_jobs
SET status = 'ready', archive = $2, size_bytes = $3,
    finished_at = $4, expires_at = $5, error = NULL
WHERE id = $1;

-- name: FailExportJob :exec
UPDATE export_jobs SET status = 'failed', error = $2, finished_at = $3 WHERE id = $1;

-- name: LatestExportJob :one
SELECT id, workspace_id, status, size_bytes, error, created_at, finished_at, expires_at
FROM export_jobs
WHERE workspace_id = $1 AND kind = 'account'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetExportArchive :one
-- The download, scoped to the workspace: the id alone is not a
-- capability, because the route also requires a session here.
SELECT id, archive, size_bytes, expires_at
FROM export_jobs
WHERE id = $1 AND workspace_id = $2 AND status = 'ready' AND kind = 'account';

-- name: CreateClosureExportJob :one
-- The copy sent when an account closes. Its expiry is set now, from the
-- moment of closure, and the token is the download's only credential.
-- A second job for the same workspace is refused by the unique index and
-- returns no row.
INSERT INTO export_jobs (workspace_id, requested_by, status, created_at, expires_at,
                         kind, download_token_hash)
VALUES ($1, $2, 'pending', $3, $4, 'closure', $5)
ON CONFLICT (workspace_id) WHERE kind = 'closure' DO NOTHING
RETURNING id, workspace_id, status, size_bytes, error, created_at, finished_at, expires_at;

-- name: DeleteExportJob :exec
DELETE FROM export_jobs WHERE id = $1;

-- name: GetClosureArchive :one
-- The bearer download: found by the token's hash alone, and only for a
-- built closure export.
SELECT id, archive, expires_at
FROM export_jobs
WHERE download_token_hash = $1 AND kind = 'closure' AND status = 'ready'
  AND archive IS NOT NULL;
