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
