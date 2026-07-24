-- M4-T3/T4: participants, invites, suppressions.

-- name: ImportParticipant :execrows
-- ON CONFLICT keeps re-imports and duplicate rows in one paste idempotent
-- (M4-T3: duplicate email import deduped). The token minted here is a
-- placeholder; the real one is minted at send time, because only the hash
-- is ever stored and the emailed link needs the raw token.
INSERT INTO participants (survey_id, email, token_hash)
VALUES ($1, $2, $3)
ON CONFLICT (survey_id, email) DO NOTHING;

-- name: ListParticipants :many
SELECT p.*, (s.email IS NOT NULL)::boolean AS suppressed
FROM participants p
LEFT JOIN suppressions s ON s.email = p.email
WHERE p.survey_id = $1 AND p.deleted_at IS NULL
ORDER BY p.created_at, p.email;

-- name: PendingInvites :many
-- Who still needs an invite: never invited, not bounced, not suppressed.
SELECT p.* FROM participants p
WHERE p.survey_id = $1
  AND p.deleted_at IS NULL
  AND p.invited_at IS NULL
  AND p.bounced_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM suppressions s WHERE s.email = p.email)
ORDER BY p.created_at
LIMIT $2;

-- name: SetParticipantTokenAndInvited :exec
UPDATE participants SET token_hash = $2, invited_at = $3 WHERE id = $1;

-- name: ClearParticipantInvited :exec
-- Rolls a failed send back to pending so the next run retries it.
UPDATE participants SET invited_at = NULL WHERE id = $1;

-- name: MarkParticipantEmailBounced :exec
-- By address across all surveys: a hard bounce means the mailbox is gone,
-- not that one survey's invite failed.
UPDATE participants SET bounced_at = $2
WHERE email = $1 AND bounced_at IS NULL;

-- name: AddSuppression :exec
INSERT INTO suppressions (email, reason)
VALUES ($1, $2)
ON CONFLICT (email) DO NOTHING;
