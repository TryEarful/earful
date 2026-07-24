-- +goose Up

-- M4-T4/T6: the suppression list. One row per address we must never mail
-- again (hard bounce, spam complaint), fed by ESP webhooks. Keyed by
-- email, not participant: a bounce suppresses the address everywhere,
-- across surveys and workspaces — deliverability is a shared resource.
CREATE TABLE suppressions (
    email      text PRIMARY KEY,
    reason     text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE suppressions;
