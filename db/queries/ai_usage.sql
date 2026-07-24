-- name: AddAIUsage :exec
INSERT INTO ai_usage (workspace_id, survey_id, kind, tokens, est_cost, day)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: WorkspaceTokensOnDay :one
SELECT coalesce(sum(tokens), 0)::bigint FROM ai_usage
WHERE workspace_id = $1 AND day = $2;

-- name: GlobalCostOnDay :one
SELECT coalesce(sum(est_cost), 0)::float8 FROM ai_usage WHERE day = $1;
