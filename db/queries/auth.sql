-- M2: users, workspaces, sessions, magic links. Every workspace-scoped
-- query in later milestones takes an explicit workspace_id parameter;
-- these auth queries are the only place that resolves "who am I".

-- name: CreateUser :one
INSERT INTO users (email, google_sub)
VALUES ($1, $2)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1 AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetUserByGoogleSub :one
SELECT * FROM users
WHERE google_sub = $1 AND deleted_at IS NULL;

-- name: SetUserGoogleSub :exec
UPDATE users SET google_sub = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: CreateWorkspace :one
INSERT INTO workspaces (name)
VALUES ($1)
RETURNING *;

-- name: CreateWorkspaceMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role)
VALUES ($1, $2, 'owner');

-- name: GetWorkspaceForUser :one
SELECT w.* FROM workspaces w
JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = $1 AND w.deleted_at IS NULL
ORDER BY w.created_at
LIMIT 1;

-- name: SoftDeleteWorkspacesForUser :exec
UPDATE workspaces SET deleted_at = $2
WHERE deleted_at IS NULL
  AND id IN (SELECT workspace_id FROM workspace_members WHERE user_id = $1);

-- name: CreateSession :one
INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: AuthenticateSession :one
SELECT s.id AS session_id, s.csrf_token, s.expires_at,
       u.id AS user_id, u.email, u.is_super_admin,
       w.id AS workspace_id, w.name AS workspace_name
FROM sessions s
JOIN users u ON u.id = s.user_id AND u.deleted_at IS NULL
JOIN workspace_members m ON m.user_id = u.id
JOIN workspaces w ON w.id = m.workspace_id AND w.deleted_at IS NULL
WHERE s.token_hash = $1
ORDER BY w.created_at
LIMIT 1;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: CreateMagicLinkToken :exec
INSERT INTO magic_link_tokens (token_hash, email, expires_at)
VALUES ($1, $2, $3);

-- name: GetMagicLinkToken :one
SELECT * FROM magic_link_tokens WHERE token_hash = $1;

-- name: ConsumeMagicLinkToken :one
UPDATE magic_link_tokens SET used_at = $2
WHERE token_hash = $1 AND used_at IS NULL
RETURNING *;

-- name: CountRecentMagicLinksForEmail :one
SELECT count(*) FROM magic_link_tokens
WHERE email = $1 AND created_at > $2;

-- M12: private beta — password auth, email change, invite codes, super
-- admin. Passwords are bcrypt-encoded strings; codes are SHA-256 hashes.

-- name: CreateUserWithPassword :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING *;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateUserEmail :exec
UPDATE users SET email = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: SetUserSuperAdmin :one
UPDATE users SET is_super_admin = $2
WHERE email = $1 AND deleted_at IS NULL
RETURNING id;

-- name: CreateBetaCode :one
INSERT INTO beta_codes (code_hash, label)
VALUES ($1, $2)
RETURNING id;

-- name: GetActiveBetaCodeForUpdate :one
-- Validate-and-lock an unused invite code BEFORE any account work, so a
-- request that lacks a valid code learns nothing about which emails exist
-- (closes the signup enumeration oracle). FOR UPDATE serialises this
-- against ConsumeBetaCode below so two racing signups cannot both consume
-- one code.
SELECT id FROM beta_codes
WHERE code_hash = $1 AND used_at IS NULL AND revoked_at IS NULL
FOR UPDATE;

-- name: ConsumeBetaCode :one
UPDATE beta_codes SET used_at = $2, used_by = $3
WHERE code_hash = $1 AND used_at IS NULL AND revoked_at IS NULL
RETURNING id;

-- name: ListBetaCodes :many
SELECT c.id, c.label, c.created_at, c.used_at, c.revoked_at,
       u.email AS used_by_email
FROM beta_codes c
LEFT JOIN users u ON u.id = c.used_by
ORDER BY c.created_at DESC;

-- name: RevokeBetaCode :one
UPDATE beta_codes SET revoked_at = $2
WHERE id = $1 AND used_at IS NULL AND revoked_at IS NULL
RETURNING id;
