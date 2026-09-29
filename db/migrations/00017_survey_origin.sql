-- How a survey came to exist (story 86, ADR-0015).
--
-- A workspace is created holding one published survey, the Starter
-- Survey. It is the owner's in every respect, so nothing about how it
-- behaves depends on this column. Two things do need to tell it from a
-- survey somebody made: the founder metrics, which would otherwise count
-- one survey created and one published for every signup, and the rule
-- that a workspace is given a Starter Survey once.
--
-- The title cannot serve: the owner is invited to change it. The column
-- is written when the row is inserted and no query updates it.
--
-- The index makes "at most one live Starter Survey per workspace" a fact
-- of the database. A deleted one does not count, so an owner who deleted
-- theirs can be given another.

-- +goose Up
ALTER TABLE surveys ADD COLUMN origin text NOT NULL DEFAULT 'creator'
    CONSTRAINT surveys_origin_known CHECK (origin IN ('creator', 'starter'));

CREATE UNIQUE INDEX surveys_one_live_starter_idx ON surveys (workspace_id)
    WHERE origin = 'starter' AND deleted_at IS NULL;

COMMENT ON COLUMN surveys.origin IS
    'How the survey came to exist: made by a creator, or seeded with the workspace (ADR-0015). Never updated.';

-- +goose Down
DROP INDEX surveys_one_live_starter_idx;
ALTER TABLE surveys DROP COLUMN origin;
