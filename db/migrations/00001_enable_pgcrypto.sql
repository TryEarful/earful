-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- +goose Down
-- deliberately no-op: dropping the extension risks breaking objects added
-- by later migrations that depend on it (e.g. gen_random_uuid() defaults).
