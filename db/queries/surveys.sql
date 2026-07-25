-- M3: surveys, drafts, revisions, versions, questions.
--
-- Every survey-scoped query takes workspace_id explicitly. That is the
-- authorization boundary (ADR-0002): a handler cannot forget it, because
-- the query will not compile without it.

-- name: CreateSurvey :one
INSERT INTO surveys (workspace_id, title, is_anonymous, close_at, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSurveyForWorkspace :one
SELECT * FROM surveys
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: ListSurveysForWorkspace :many
SELECT s.*,
       -- coalesce+cast so sqlc infers a concrete type (a bare max() over
       -- no rows is untyped NULL to it); 0 means never published.
       coalesce((SELECT max(v.number) FROM survey_versions v WHERE v.survey_id = s.id), 0)::int AS latest_version,
       (SELECT count(*) FROM questions q
          JOIN survey_versions v ON v.id = q.version_id
         WHERE v.survey_id = s.id
           AND v.number = (SELECT max(v2.number) FROM survey_versions v2 WHERE v2.survey_id = s.id)
       ) AS latest_question_count
FROM surveys s
WHERE s.workspace_id = $1 AND s.deleted_at IS NULL
ORDER BY s.created_at DESC;

-- name: UpdateSurveySettings :exec
-- Deliberately cannot touch is_anonymous; the database refuses it anyway
-- (ADR-0003 trigger), but the query shape means no caller can even try.
UPDATE surveys SET title = $3, close_at = $4
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: SetSurveyClosedAt :exec
UPDATE surveys SET closed_at = $3
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteSurvey :exec
UPDATE surveys SET deleted_at = $3
WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL;

-- name: CreateDraft :one
INSERT INTO survey_drafts (survey_id, structure, updated_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetDraftForSurvey :one
SELECT * FROM survey_drafts WHERE survey_id = $1;

-- name: UpdateDraftStructure :one
UPDATE survey_drafts SET structure = $2, updated_by = $3, updated_at = $4
WHERE survey_id = $1
RETURNING *;

-- name: CreateDraftRevision :exec
INSERT INTO draft_revisions (draft_id, structure, saved_by, saved_at)
VALUES ($1, $2, $3, $4);

-- name: ListDraftRevisions :many
SELECT r.id, r.saved_at, r.structure, u.email AS saved_by_email
FROM draft_revisions r
LEFT JOIN users u ON u.id = r.saved_by
WHERE r.draft_id = $1
ORDER BY r.saved_at DESC, r.id DESC;

-- name: NextVersionNumber :one
SELECT coalesce(max(number), 0) + 1 AS next FROM survey_versions WHERE survey_id = $1;

-- name: CreateVersion :one
INSERT INTO survey_versions (survey_id, number, published_by, published_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetLatestVersion :one
SELECT * FROM survey_versions WHERE survey_id = $1 ORDER BY number DESC LIMIT 1;

-- name: GetVersion :one
SELECT * FROM survey_versions WHERE id = $1 AND survey_id = $2;

-- name: ListVersions :many
SELECT v.id, v.number, v.published_at, u.email AS published_by_email
FROM survey_versions v
LEFT JOIN users u ON u.id = v.published_by
WHERE v.survey_id = $1
ORDER BY v.number DESC;

-- name: EnsureQuestionIdentity :exec
-- Identities are minted in Go when a question first appears in a draft and
-- only reach the database at publish. ON CONFLICT keeps republishing an
-- unchanged question idempotent.
INSERT INTO question_identities (id, survey_id)
VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING;

-- name: CreateQuestion :one
INSERT INTO questions (version_id, question_identity_id, type, text, options, required, position,
                       scale_min, scale_max)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListQuestionsForVersion :many
SELECT * FROM questions WHERE version_id = $1 ORDER BY position;
