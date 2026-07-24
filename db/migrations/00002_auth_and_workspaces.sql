-- +goose Up

-- M2: users, workspaces, memberships, sessions, magic-link tokens.
-- Soft-delete columns (deleted_at) follow PLAN.md Appendix A; hard
-- deletion happens only in the purge subcommand (M8).

CREATE TABLE users (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email      text NOT NULL,
    google_sub text,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

-- Partial unique indexes: a soft-deleted account's email (or Google
-- subject) may sign up again as a brand-new user while the old rows wait
-- out the 30-day purge window. Uniqueness only applies to live rows.
CREATE UNIQUE INDEX users_email_live_uniq ON users (email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_google_sub_live_uniq ON users (google_sub) WHERE deleted_at IS NULL;

CREATE TABLE workspaces (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE workspace_members (
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    user_id      uuid NOT NULL REFERENCES users (id),
    role         text NOT NULL DEFAULT 'owner',
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX workspace_members_user_idx ON workspace_members (user_id);

-- Server-side sessions. The cookie carries a 256-bit random token; only
-- its SHA-256 hash is stored, so a database leak cannot be replayed into
-- live sessions. csrf_token is the per-session synchronizer token
-- embedded in every mutating form.
CREATE TABLE sessions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash bytea NOT NULL UNIQUE,
    user_id    uuid NOT NULL REFERENCES users (id),
    csrf_token text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX sessions_user_idx ON sessions (user_id);

-- Magic-link login tokens, hashed at rest (PLAN.md M2-T3): the emailed
-- link carries the raw token; the row stores SHA-256 only. Single-use via
-- used_at; 15-minute expiry enforced in code against the injectable clock.
CREATE TABLE magic_link_tokens (
    token_hash bytea PRIMARY KEY,
    email      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE INDEX magic_link_tokens_email_idx ON magic_link_tokens (email);

-- +goose Down
DROP TABLE magic_link_tokens;
DROP TABLE sessions;
DROP TABLE workspace_members;
DROP TABLE workspaces;
DROP TABLE users;
