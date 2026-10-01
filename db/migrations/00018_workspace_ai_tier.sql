-- A workspace's AI allowance tier (issue #3).
--
-- The workspace is the unit the AI meter already caps per day; the tier
-- chooses which cap applies. The caps themselves are configuration
-- (AI_TIER_*_DAILY_TOKENS), not data, so an operator can retune every
-- tier at once without touching a row. Only a super admin changes a
-- workspace's tier.

-- +goose Up
ALTER TABLE workspaces ADD COLUMN ai_tier text NOT NULL DEFAULT 'normal'
    CONSTRAINT workspaces_ai_tier_known CHECK (ai_tier IN ('low_normal', 'normal', 'high'));

COMMENT ON COLUMN workspaces.ai_tier IS
    'Which daily AI token cap applies to the workspace: low_normal, normal or high. Set by a super admin.';

-- +goose Down
ALTER TABLE workspaces DROP COLUMN ai_tier;
