-- M9-T7: founder metrics, from our own database.
--
-- Nothing here is added to a respondent page and no third-party
-- analytics exists to add (ADR-0006). These are counts of the product's
-- own objects, read by a super admin.

-- name: MetricTotals :one
-- A Starter Survey (ADR-0015) is created and published with its
-- workspace, by nobody. Counted as it stands it would add one survey
-- created and one published to every signup, and both numbers would
-- follow Accounts and say nothing of their own. It is counted from its
-- second version, which is the first a person published.
SELECT
    (SELECT count(*) FROM users WHERE deleted_at IS NULL)::bigint      AS users,
    (SELECT count(*) FROM workspaces WHERE deleted_at IS NULL)::bigint AS workspaces,
    (SELECT count(*) FROM surveys s
      WHERE s.deleted_at IS NULL
        AND (s.origin <> 'starter' OR EXISTS (
            SELECT 1 FROM survey_versions v
             WHERE v.survey_id = s.id AND v.number > 1)))::bigint      AS surveys,
    (SELECT count(DISTINCT v.survey_id) FROM survey_versions v
       JOIN surveys s ON s.id = v.survey_id
      WHERE s.origin <> 'starter' OR v.number > 1)::bigint             AS published_surveys,
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
-- counters (ADR-0009) — never from anything joined to a response. The
-- undated totals and the per-day rows (ADR-0012) are added together:
-- a survey's opens live in one table or the other, never both.
SELECT
    (coalesce((SELECT sum(count) FROM survey_stats WHERE metric = 'start'), 0)
     + coalesce((SELECT sum(count) FROM survey_stats_daily WHERE metric = 'start'), 0))::bigint AS starts,
    (coalesce((SELECT sum(count) FROM survey_stats WHERE metric = 'completion'), 0)
     + coalesce((SELECT sum(count) FROM survey_stats_daily WHERE metric = 'completion'), 0))::bigint AS completions;
