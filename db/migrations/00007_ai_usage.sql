-- +goose Up

-- M6-T2: AI usage accounting. One row per AI call; the per-workspace
-- daily cap and the global daily € breaker are both sums over this table,
-- so they survive restarts and need no separate counters.
CREATE TABLE ai_usage (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid REFERENCES workspaces (id),
    survey_id    uuid REFERENCES surveys (id),
    kind         text NOT NULL,     -- generate | transcribe | translate | analyze
    tokens       bigint NOT NULL,
    est_cost     numeric NOT NULL,  -- euros, estimated
    day          date NOT NULL
);

CREATE INDEX ai_usage_workspace_day_idx ON ai_usage (workspace_id, day);
CREATE INDEX ai_usage_day_idx ON ai_usage (day);

-- +goose Down
DROP TABLE ai_usage;
