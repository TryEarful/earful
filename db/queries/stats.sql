-- M7-T4: survey stats (ADR-0009).
--
-- Every query here touches survey_stats alone. None of them may mention
-- responses or answers: a counter that could be joined to a response is
-- no longer an aggregate, and TestAggregatesCannotBeLinkedToResponses
-- fails the build if one appears.

-- name: IncrementSurveyStat :exec
INSERT INTO survey_stats (survey_id, metric, bucket, count)
VALUES ($1, $2, $3, 1)
ON CONFLICT (survey_id, metric, bucket)
DO UPDATE SET count = survey_stats.count + 1;

-- name: ListSurveyStats :many
SELECT metric, bucket, count FROM survey_stats
WHERE survey_id = $1
ORDER BY metric, count DESC, bucket;

-- name: DeleteSurveyStats :exec
DELETE FROM survey_stats WHERE survey_id = $1;

-- Daily flow counters (ADR-0012). Same rule: survey_stats_daily alone,
-- never joined to anything a respondent wrote. The scan test matches on
-- the "survey_stats" prefix, so it guards this table without being told.

-- name: IncrementSurveyStatDaily :exec
INSERT INTO survey_stats_daily (survey_id, metric, bucket, day, count)
VALUES ($1, $2, $3, $4, 1)
ON CONFLICT (survey_id, metric, bucket, day)
DO UPDATE SET count = survey_stats_daily.count + 1;

-- name: ListSurveyStatsDaily :many
-- Rows for one survey between two days inclusive.
SELECT metric, bucket, day, count FROM survey_stats_daily
WHERE survey_id = $1 AND day >= sqlc.arg(from_day) AND day <= sqlc.arg(to_day)
ORDER BY day, metric, bucket;

-- name: ListAllSurveyStatsDaily :many
-- Every dated row a survey has, for the workspace export.
SELECT metric, bucket, day, count FROM survey_stats_daily
WHERE survey_id = $1
ORDER BY day, metric, bucket;
