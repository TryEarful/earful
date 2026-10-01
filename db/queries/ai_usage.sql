-- name: AddAIUsage :exec
INSERT INTO ai_usage (workspace_id, survey_id, kind, tokens, est_cost, duration_secs, day)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: WorkspaceTokensOnDay :one
SELECT coalesce(sum(tokens), 0)::bigint FROM ai_usage
WHERE workspace_id = $1 AND day = $2;

-- name: GlobalCostOnDay :one
SELECT coalesce(sum(est_cost), 0)::float8 FROM ai_usage WHERE day = $1;

-- name: SurveyVoiceSecondsOnDay :one
-- The per-survey daily voice cap (M5-T4): how many seconds of speech this
-- survey has had transcribed today, across every respondent.
SELECT coalesce(sum(duration_secs), 0)::bigint FROM ai_usage
WHERE survey_id = $1 AND day = $2;

-- name: WorkspaceAITier :one
SELECT ai_tier FROM workspaces WHERE id = $1;

-- name: SetWorkspaceAITier :execrows
UPDATE workspaces SET ai_tier = $2 WHERE id = $1 AND deleted_at IS NULL;

-- name: WorkspacesForAITier :many
-- The super-admin tier control finds workspaces by a member's address,
-- the same way the other support tools find an account. Addresses are
-- stored lower case, so the caller lowers the one it is given.
SELECT w.id, w.name, w.ai_tier
FROM workspaces w
JOIN workspace_members m ON m.workspace_id = w.id
JOIN users u ON u.id = m.user_id
WHERE u.email = sqlc.arg(email)::text
  AND u.deleted_at IS NULL AND w.deleted_at IS NULL
ORDER BY w.created_at, w.id;
