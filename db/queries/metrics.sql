-- M9-T7: founder metrics, from our own database.
--
-- Nothing here is added to a respondent page and no third-party
-- analytics exists to add (ADR-0006). These are counts of the product's
-- own objects, read by a super admin.

-- name: MetricTotals :one
SELECT
    (SELECT count(*) FROM users WHERE deleted_at IS NULL)::bigint      AS users,
    (SELECT count(*) FROM workspaces WHERE deleted_at IS NULL)::bigint AS workspaces,
    (SELECT count(*) FROM surveys WHERE deleted_at IS NULL)::bigint    AS surveys,
    (SELECT count(DISTINCT survey_id) FROM survey_versions)::bigint    AS published_surveys,
    (SELECT count(*) FROM responses WHERE deleted_at IS NULL)::bigint  AS responses,
    (SELECT count(*) FROM participants WHERE deleted_at IS NULL)::bigint AS participants;

-- name: MetricSignupsByDay :many
SELECT created_at::date AS day, count(*)::bigint AS count
FROM users
WHERE created_at >= $1 AND deleted_at IS NULL
GROUP BY 1 ORDER BY 1;

-- name: MetricResponsesByDay :many
SELECT submitted_at::date AS day, count(*)::bigint AS count
FROM responses
WHERE submitted_at >= $1 AND deleted_at IS NULL
GROUP BY 1 ORDER BY 1;

-- name: MetricAICostByDay :many
SELECT day, sum(tokens)::bigint AS tokens, sum(est_cost)::float8 AS cost
FROM ai_usage
WHERE day >= $1
GROUP BY day ORDER BY day;

-- name: MetricCompletionRates :one
-- Starts and completions across every survey, from the unlinked
-- counters (ADR-0009) — never from anything joined to a response.
SELECT
    coalesce(sum(count) FILTER (WHERE metric = 'start'), 0)::bigint      AS starts,
    coalesce(sum(count) FILTER (WHERE metric = 'completion'), 0)::bigint AS completions
FROM survey_stats;
