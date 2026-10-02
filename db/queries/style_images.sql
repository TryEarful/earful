-- The pictures of a survey's style (ADR-0018, migration 00025).
--
-- A style refers to a picture by the hash of its bytes, in hex, wherever
-- in the style it sits: the jsonpath below finds a reference at any
-- depth, so a further place for a picture needs no new query.

-- name: CreateSurveyImage :exec
-- The same upload twice is one row.
INSERT INTO survey_images (survey_id, sha256, content_type, width, height, size_bytes, bytes, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (survey_id, sha256) DO NOTHING;

-- name: CountSurveyImages :one
SELECT count(*) FROM survey_images WHERE survey_id = $1;

-- name: SurveyImageExists :one
SELECT EXISTS (SELECT 1 FROM survey_images WHERE survey_id = $1 AND sha256 = $2);

-- name: GetSurveyImageForWorkspace :one
-- The creator's own view of a picture, for the Style tab and the
-- preview: any picture stored for a survey of their workspace.
SELECT i.content_type, i.bytes
FROM survey_images i
JOIN surveys s ON s.id = i.survey_id
WHERE i.survey_id = $1 AND i.sha256 = $2
  AND s.workspace_id = $3 AND s.deleted_at IS NULL;

-- name: GetPublishedSurveyImage :one
-- What anybody may fetch: a picture that a published version of a survey
-- that has not been deleted shows. A picture only a draft refers to is
-- not public yet, and a deleted survey's is public no longer; nor is
-- the picture of a survey whose workspace is suspended, while it is.
SELECT i.content_type, i.bytes
FROM survey_images i
JOIN surveys s ON s.id = i.survey_id
JOIN workspaces w ON w.id = s.workspace_id
WHERE i.sha256 = $1
  AND s.deleted_at IS NULL
  AND w.suspended_at IS NULL
  AND EXISTS (
      SELECT 1 FROM survey_versions v
      WHERE v.survey_id = i.survey_id
        AND v.style IS NOT NULL
        AND jsonb_path_exists(v.style, '$.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  )
LIMIT 1;

-- name: LockSurveyForStyle :one
-- Holds a survey while its pictures and its draft change together, or
-- while it is published: two saves of one survey's style, or a save and a
-- publish, take turns, and the purge leaves a survey held this way alone.
-- NO KEY UPDATE, not UPDATE: a response, and the counts beside it, refer
-- to the survey row, and checking that reference takes a lock that
-- FOR UPDATE would make wait. Respondents are never held up by a save.
SELECT id FROM surveys WHERE id = $1 FOR NO KEY UPDATE;

-- name: CountSurveyImagesOf :one
-- How many of the given pictures a survey stores.
SELECT count(*) FROM survey_images
WHERE survey_id = $1 AND encode(sha256, 'hex') = ANY(sqlc.arg(hashes)::text[]);

-- name: GetPublishedSurveyImageMeta :one
-- The same as GetPublishedSurveyImage, without the bytes: enough to say
-- whether anybody may fetch a picture, and how large it is, for a browser
-- asking whether what it kept is current, or for its headers alone.
SELECT i.content_type, i.size_bytes
FROM survey_images i
JOIN surveys s ON s.id = i.survey_id
JOIN workspaces w ON w.id = s.workspace_id
WHERE i.sha256 = $1
  AND s.deleted_at IS NULL
  AND w.suspended_at IS NULL
  AND EXISTS (
      SELECT 1 FROM survey_versions v
      WHERE v.survey_id = i.survey_id
        AND v.style IS NOT NULL
        AND jsonb_path_exists(v.style, '$.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  )
LIMIT 1;

-- name: DeleteUnusedSurveyImages :execrows
-- Removes the pictures of one survey that nothing shows: no published
-- version, not the current draft, and not the style being saved (the
-- pictures in keep). It makes room when a survey is at its limit of
-- stored pictures.
DELETE FROM survey_images i
WHERE i.survey_id = sqlc.arg(survey_id)
  AND NOT (encode(i.sha256, 'hex') = ANY(sqlc.arg(keep)::text[]))
  AND NOT EXISTS (
      SELECT 1 FROM survey_versions v
      WHERE v.survey_id = i.survey_id
        AND v.style IS NOT NULL
        AND jsonb_path_exists(v.style, '$.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  )
  AND NOT EXISTS (
      SELECT 1 FROM survey_drafts d
      WHERE d.survey_id = i.survey_id
        AND jsonb_path_exists(d.structure, '$.style.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  );

-- name: ListPublishedImagesForSurvey :many
-- The pictures a survey's published versions show, for the workspace
-- export.
SELECT i.sha256, i.content_type, i.bytes
FROM survey_images i
WHERE i.survey_id = $1
  AND EXISTS (
      SELECT 1 FROM survey_versions v
      WHERE v.survey_id = i.survey_id
        AND v.style IS NOT NULL
        AND jsonb_path_exists(v.style, '$.** ? (@.sha256 == $hash)',
                              jsonb_build_object('hash', encode(i.sha256, 'hex')))
  )
ORDER BY i.created_at, i.id;
