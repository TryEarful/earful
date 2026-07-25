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
