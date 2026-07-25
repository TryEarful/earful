-- M4: the respondent path.
--
-- Nothing here writes an IP address or user agent: no such column exists
-- on responses or answers (ADR-0003), so the anonymity promise cannot be
-- broken by a careless query.

-- name: GetPublicSurvey :one
-- The respondent-facing lookup: by id alone, with no workspace scoping,
-- because a share link is the credential. Soft-deleted surveys vanish.
SELECT s.id, s.title, s.is_anonymous, s.close_at, s.closed_at,
       s.workspace_id, w.name AS workspace_name,
       coalesce((SELECT max(v.number) FROM survey_versions v WHERE v.survey_id = s.id), 0)::int AS latest_version
FROM surveys s
JOIN workspaces w ON w.id = s.workspace_id AND w.deleted_at IS NULL
WHERE s.id = $1 AND s.deleted_at IS NULL;

-- name: CreateResponse :one
INSERT INTO responses (survey_id, version_id, participant_id, duration_secs, submitted_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateAnswer :exec
INSERT INTO answers (response_id, question_id, question_identity_id, value)
VALUES ($1, $2, $3, $4);

-- name: CountResponsesForSurvey :one
SELECT count(*) FROM responses WHERE survey_id = $1 AND deleted_at IS NULL;

-- name: GetParticipantByTokenHash :one
SELECT p.*, s.id AS survey_id_check
FROM participants p
JOIN surveys s ON s.id = p.survey_id AND s.deleted_at IS NULL
WHERE p.token_hash = $1 AND p.deleted_at IS NULL;

-- name: MarkParticipantSubmitted :exec
UPDATE participants SET submitted_at = $2 WHERE id = $1;

-- name: SoftDeleteResponse :execrows
-- M8-T1: a creator can remove a response; support can restore it until
-- the purge job hard-deletes it 30 days later.
UPDATE responses SET deleted_at = $3
WHERE id = $1 AND survey_id = $2 AND deleted_at IS NULL;
