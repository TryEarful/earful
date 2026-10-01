-- A copy of an account's data, sent when the account is closed.
--
-- 00012 made the export link deliberately not a bearer token: the
-- download route requires a session in the owning workspace. A closure
-- export cannot work that way, because closing the account revokes every
-- session and soft-deletes the workspace before the archive exists. Its
-- link is therefore a bearer capability, emailed to the address on the
-- account, and the rules a bearer capability needs are written here:
--
--   - Only SHA-256 of the token is stored, as for magic links, so a copy
--     of the database cannot be replayed into a download.
--   - Only a closure job carries a token, and every closure job carries
--     one. An account export keeps the session-scoped route and no token.
--   - A workspace is closed once, so it has at most one closure export.
--     The index also settles two submits of the same closure form racing
--     each other.
--
-- A closure export expires 7 days after the account closes, which the
-- row's expires_at records when it is created. The purge needs no change
-- for it: expired archives are dropped by each row's own expires_at, and
-- the jobs of a deleted workspace go with the workspace after the 30-day
-- window, which is longer.

-- +goose Up
ALTER TABLE export_jobs
    ADD COLUMN kind text NOT NULL DEFAULT 'account',
    ADD COLUMN download_token_hash bytea,
    ADD CONSTRAINT export_jobs_kind_known CHECK (kind IN ('account', 'closure')),
    ADD CONSTRAINT export_jobs_token_only_for_closure
        CHECK ((kind = 'closure') = (download_token_hash IS NOT NULL));

CREATE UNIQUE INDEX export_jobs_one_closure_idx ON export_jobs (workspace_id)
    WHERE kind = 'closure';

CREATE UNIQUE INDEX export_jobs_download_token_idx ON export_jobs (download_token_hash)
    WHERE download_token_hash IS NOT NULL;

COMMENT ON COLUMN export_jobs.download_token_hash IS
    'SHA-256 of the emailed download token of a closure export. The token itself is never stored.';

-- +goose Down
DROP INDEX export_jobs_download_token_idx;
DROP INDEX export_jobs_one_closure_idx;
ALTER TABLE export_jobs
    DROP CONSTRAINT export_jobs_token_only_for_closure,
    DROP CONSTRAINT export_jobs_kind_known,
    DROP COLUMN download_token_hash,
    DROP COLUMN kind;
