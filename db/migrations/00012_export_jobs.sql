-- M7-T3: workspace export — the "leave anytime" promise, made real.
--
-- The archive lives in Postgres rather than in object storage (ADR-0010).
-- A self-hoster running `docker compose up` gets the same export as the
-- SaaS with nothing else to configure, and the SaaS gains no bucket, no
-- signed-URL machinery and no lifecycle rule to get wrong. The cost is
-- that a workspace's export must fit comfortably in a row, which is why
-- the builder caps it and says so when it doesn't.
--
-- The download link is the job id, and it expires. It is deliberately
-- not a bearer token: the route requires a session in the owning
-- workspace, so a link that leaks is not a workspace that leaks — and a
-- token nobody can share adds nothing over the id.

-- +goose Up
CREATE TABLE export_jobs (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    requested_by uuid REFERENCES users (id),
    -- pending → running → ready | failed
    status       text NOT NULL DEFAULT 'pending',
    archive      bytea,
    size_bytes   bigint NOT NULL DEFAULT 0,
    error        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    expires_at   timestamptz,
    CONSTRAINT export_jobs_status_known CHECK (status IN ('pending', 'running', 'ready', 'failed'))
);

CREATE INDEX export_jobs_workspace_idx ON export_jobs (workspace_id, created_at DESC);

-- +goose Down
DROP TABLE export_jobs;
