-- A workspace an operator has suspended (ADR-0018, its safeguards).
--
-- Suspension is how an operator stops a workspace that is misusing the
-- service, a survey that impersonates somebody first among them, without
-- erasing anything. While it lasts the workspace's surveys take no
-- answers and show none of their style, and its creators can read and
-- export what they have but cannot publish, reopen, send invitations or
-- use AI. Lifting it puts everything back as it was.
--
-- It is not deletion: deleted_at is untouched, so the purge, which
-- erases what was deleted, does not start counting. The three columns
-- are set and cleared together. Who suspended a workspace and why is
-- also written to the application log when it happens, and again when
-- it is lifted, so the history outlives the columns.

-- +goose Up
ALTER TABLE workspaces
    ADD COLUMN suspended_at     timestamptz,
    ADD COLUMN suspended_reason text,
    ADD COLUMN suspended_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD CONSTRAINT workspaces_suspension_whole CHECK (
        (suspended_at IS NULL) = (suspended_reason IS NULL)
        AND (suspended_at IS NOT NULL OR suspended_by IS NULL)
    ),
    ADD CONSTRAINT workspaces_suspension_reason_given CHECK (
        suspended_reason IS NULL OR length(btrim(suspended_reason)) > 0
    );

COMMENT ON COLUMN workspaces.suspended_at IS
    'When an operator suspended the workspace; NULL while it is not suspended.';
COMMENT ON COLUMN workspaces.suspended_reason IS
    'The operator''s reason for the suspension, as they wrote it.';
COMMENT ON COLUMN workspaces.suspended_by IS
    'The super admin who suspended the workspace, while their account exists.';

-- +goose Down
ALTER TABLE workspaces
    DROP CONSTRAINT workspaces_suspension_reason_given,
    DROP CONSTRAINT workspaces_suspension_whole,
    DROP COLUMN suspended_by,
    DROP COLUMN suspended_reason,
    DROP COLUMN suspended_at;
