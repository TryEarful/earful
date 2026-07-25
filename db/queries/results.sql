-- M7: reading results.
--
-- Everything here aggregates by question_identity_id, which `answers`
-- carries denormalised for exactly this purpose: a response stays pinned
-- to the version it was served (ADR-0001), and results are assembled at
-- read time across versions rather than by copying anything forward.

-- name: ListQuestionsAcrossVersions :many
-- Every version's questions, oldest version first. The caller folds them
-- by identity to get the current wording plus the history of how it was
-- worded when each response was collected.
SELECT q.question_identity_id, q.type, q.text, q.options, q.required,
       q.position, q.scale_min, q.scale_max, v.number AS version_number
FROM questions q
JOIN survey_versions v ON v.id = q.version_id
WHERE v.survey_id = $1
ORDER BY v.number, q.position;

-- name: ListAnswersForSurvey :many
-- One row per stored answer, with the version it was given under and the
-- participant it belongs to (NULL forever for anonymous surveys).
-- Skipped questions store no row at all, which is what keeps "skipped"
-- and "answered blank" distinguishable.
SELECT a.id AS answer_id, a.response_id, a.question_identity_id, a.value,
       v.number AS version_number, r.submitted_at, r.duration_secs,
       p.email AS participant_email
FROM answers a
JOIN responses r ON r.id = a.response_id
JOIN survey_versions v ON v.id = r.version_id
LEFT JOIN participants p ON p.id = r.participant_id
WHERE r.survey_id = $1 AND r.deleted_at IS NULL
ORDER BY r.submitted_at, a.question_identity_id;

-- name: ListResponsesForSurvey :many
-- The response rows themselves, so a table can show one row per response
-- including responses that answered nothing.
SELECT r.id, v.number AS version_number, r.submitted_at, r.duration_secs,
       p.email AS participant_email
FROM responses r
JOIN survey_versions v ON v.id = r.version_id
LEFT JOIN participants p ON p.id = r.participant_id
WHERE r.survey_id = $1 AND r.deleted_at IS NULL
ORDER BY r.submitted_at;
