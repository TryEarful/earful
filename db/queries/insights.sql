-- M10: Insight Summaries. Append-only by trigger; the newest run for a
-- survey is the one shown, and its watermark is the cache key.

-- name: CreateInsightRun :one
INSERT INTO insight_runs (survey_id, response_watermark, response_count, model, output, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, survey_id, response_watermark, response_count, model, output, created_at;

-- name: LatestInsightRun :one
SELECT id, survey_id, response_watermark, response_count, model, output, created_at
FROM insight_runs
WHERE survey_id = $1
ORDER BY created_at DESC
LIMIT 1;
