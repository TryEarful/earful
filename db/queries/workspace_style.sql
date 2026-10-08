-- The account's style and its pictures (ADR-0023, migration 00027).
--
-- As with a survey's pictures, a style refers to a picture by the hash
-- of its bytes, in hex, wherever in the style it sits.

-- name: GetWorkspaceStyle :one
SELECT style, localizations, updated_at
FROM workspace_styles
WHERE workspace_id = $1;

-- name: EnsureWorkspaceStyle :exec
-- Makes the row a workspace's style is saved into, so that two saves can
-- hold the same row and take turns.
INSERT INTO workspace_styles (workspace_id, updated_at)
VALUES ($1, $2)
ON CONFLICT (workspace_id) DO NOTHING;

-- name: LockWorkspaceStyle :one
-- Holds the account's style while its pictures and its style change
-- together. NO KEY UPDATE, as a survey is held, so nothing that only
-- refers to the workspace waits on it.
SELECT workspace_id FROM workspace_styles WHERE workspace_id = $1 FOR NO KEY UPDATE;

-- name: ShareWorkspaceStyle :one
-- Reads the account's style while publishing a survey that follows it,
-- so that it does not change between being read and being frozen.
SELECT style, localizations, updated_at
FROM workspace_styles
WHERE workspace_id = $1
FOR SHARE;

-- name: UpdateWorkspaceStyle :exec
UPDATE workspace_styles
SET style = $2, updated_by = $3, updated_at = $4
WHERE workspace_id = $1;

-- name: UpdateWorkspaceStyleLocalizations :exec
UPDATE workspace_styles
SET localizations = $2, updated_by = $3, updated_at = $4
WHERE workspace_id = $1;

-- name: WorkspaceDraftLanguages :many
-- The languages the workspace's surveys are being translated into, as
-- their drafts hold them: the languages the account's style is offered
-- in for translating.
SELECT DISTINCT lang::text
FROM survey_drafts d
JOIN surveys s ON s.id = d.survey_id
CROSS JOIN LATERAL jsonb_object_keys(
    CASE WHEN jsonb_typeof(d.structure -> 'localizations') = 'object'
         THEN d.structure -> 'localizations' ELSE '{}'::jsonb END
) AS lang
WHERE s.workspace_id = $1 AND s.deleted_at IS NULL
ORDER BY lang;

-- name: CreateWorkspaceImage :exec
-- The same upload twice is one row.
INSERT INTO workspace_images (workspace_id, sha256, content_type, width, height, size_bytes, bytes, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, sha256) DO NOTHING;

-- name: WorkspaceImageExists :one
SELECT EXISTS (SELECT 1 FROM workspace_images WHERE workspace_id = $1 AND sha256 = $2);

-- name: CountWorkspaceImages :one
SELECT count(*) FROM workspace_images WHERE workspace_id = $1;

-- name: DeleteUnusedWorkspaceImages :execrows
-- Removes the account's pictures that its style does not show and that
-- the style being saved does not bring (the pictures in keep). It makes
-- room when an account is at its limit of stored pictures. A survey that
-- showed one has its own copy (migration 00027).
DELETE FROM workspace_images i
WHERE i.workspace_id = sqlc.arg(workspace_id)
  AND NOT (encode(i.sha256, 'hex') = ANY(sqlc.arg(keep)::text[]))
  AND NOT EXISTS (
      SELECT 1 FROM workspace_styles w
      WHERE w.workspace_id = i.workspace_id
        AND jsonb_path_exists(w.style, '$.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  );

-- name: GetWorkspaceImage :one
-- An account's picture, for its creator's own pages.
SELECT i.content_type, i.bytes
FROM workspace_images i
WHERE i.workspace_id = $1 AND i.sha256 = $2;

-- name: ListWorkspaceStyleImages :many
-- The pictures the account's style shows, for the workspace export.
SELECT i.sha256, i.content_type, i.bytes
FROM workspace_images i
JOIN workspace_styles w ON w.workspace_id = i.workspace_id
WHERE i.workspace_id = $1
  AND jsonb_path_exists(w.style, '$.** ? (@.sha256 == $hash)',
                        jsonb_build_object('hash', encode(i.sha256, 'hex')))
ORDER BY i.created_at, i.id;
