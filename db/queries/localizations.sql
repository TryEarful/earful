-- M11: localizations (frozen at publish) and answer translations
-- (cached, creator-side).

-- name: CreateQuestionLocalization :exec
INSERT INTO question_localizations (version_id, question_id, lang, text, options)
VALUES ($1, $2, $3, $4, $5);

-- name: ListLocalizationsForVersion :many
SELECT ql.question_id, q.question_identity_id, ql.lang, ql.text, ql.options
FROM question_localizations ql
JOIN questions q ON q.id = ql.question_id
WHERE ql.version_id = $1
ORDER BY ql.lang, q.position;

-- name: ListVersionLanguages :many
SELECT DISTINCT lang FROM question_localizations WHERE version_id = $1 ORDER BY lang;

-- name: UpsertAnswerTranslation :exec
INSERT INTO answer_translations (answer_id, lang, text, model, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (answer_id, lang) DO UPDATE
SET text = excluded.text, model = excluded.model, created_at = excluded.created_at;

-- name: ListAnswerTranslations :many
-- Every translation for a survey's answers in one language, so the
-- results page can show them beside the originals without N queries.
SELECT at.answer_id, at.lang, at.text, at.model
FROM answer_translations at
JOIN answers a ON a.id = at.answer_id
JOIN responses r ON r.id = a.response_id
WHERE r.survey_id = $1 AND at.lang = $2 AND r.deleted_at IS NULL;
