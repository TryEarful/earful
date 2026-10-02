-- Workspace suspension (ADR-0018, its safeguards; migration 00026).
-- Only a super admin suspends or lifts, from /admin/suspensions.

-- name: SuspendWorkspace :execrows
-- A live workspace that is not already suspended. Suspending one twice
-- would overwrite who did it first and why.
UPDATE workspaces
SET suspended_at = sqlc.arg(suspended_at), suspended_reason = sqlc.arg(reason)::text,
    suspended_by = sqlc.arg(suspended_by)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL AND suspended_at IS NULL;

-- name: LiftWorkspaceSuspension :execrows
UPDATE workspaces
SET suspended_at = NULL, suspended_reason = NULL, suspended_by = NULL
WHERE id = $1 AND deleted_at IS NULL AND suspended_at IS NOT NULL;

-- name: ListSuspendedWorkspaces :many
-- Every suspended workspace, the longest suspended first, with the
-- address of the member who owns it (MVP is sole membership, ADR-0002)
-- and of the operator who suspended it.
SELECT w.id, w.name, w.suspended_at::timestamptz AS suspended_at, w.suspended_reason::text AS reason,
       coalesce((SELECT u.email FROM workspace_members m JOIN users u ON u.id = m.user_id
                 WHERE m.workspace_id = w.id AND u.deleted_at IS NULL
                 ORDER BY m.created_at LIMIT 1), '')::text AS member_email,
       coalesce((SELECT u.email FROM users u WHERE u.id = w.suspended_by), '')::text AS suspended_by_email
FROM workspaces w
WHERE w.suspended_at IS NOT NULL AND w.deleted_at IS NULL
ORDER BY w.suspended_at, w.id;

-- name: WorkspacesForSuspension :many
-- The suspension control finds workspaces by a member's address, as the
-- other support tools find an account. Addresses are stored lower case,
-- so the caller lowers the one it is given.
SELECT w.id, w.name, w.suspended_at
FROM workspaces w
JOIN workspace_members m ON m.workspace_id = w.id
JOIN users u ON u.id = m.user_id
WHERE u.email = sqlc.arg(email)::text
  AND u.deleted_at IS NULL AND w.deleted_at IS NULL
ORDER BY w.created_at, w.id;

-- name: WorkspaceSuspended :one
-- Whether a workspace is suspended, for the AI meter, which every AI
-- feature asks before it spends anything.
SELECT (suspended_at IS NOT NULL)::bool AS suspended FROM workspaces WHERE id = $1;

-- name: WorkspaceLive :one
-- Tells a workspace that is not there from one already in the state a
-- suspension or a lift asked for, after an update that changed no row.
SELECT (deleted_at IS NULL)::bool AS live FROM workspaces WHERE id = $1;
