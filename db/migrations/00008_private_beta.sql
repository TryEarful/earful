-- +goose Up

-- M12: the private-beta gate. The SaaS runs invite-only and (until
-- Brevo) with zero email infrastructure, so accounts are created with a
-- one-shot invite code and thereafter authenticate with email+password.
--
-- users.password_hash is nullable on purpose: Google-created and
-- magic-link-era accounts have no password, and all three identities
-- coexist. is_super_admin gates the /admin surface (code minting,
-- password resets); it is flipped ONLY by the earful admin CLI with
-- direct database access — no web path can grant it.

ALTER TABLE users ADD COLUMN password_hash text;
ALTER TABLE users ADD COLUMN is_super_admin boolean NOT NULL DEFAULT false;

-- Invite codes, hashed at rest like every other secret (the plaintext
-- earful-XXXX-XXXX-XXXX code is shown exactly once at mint time).
-- Strictly single-use: used_at/used_by are the "mark it as used" record,
-- set atomically in the same transaction that creates the account.
CREATE TABLE beta_codes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash  bytea NOT NULL UNIQUE,
    label      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    used_at    timestamptz,
    used_by    uuid REFERENCES users (id),
    revoked_at timestamptz
);

-- +goose Down
DROP TABLE beta_codes;
ALTER TABLE users DROP COLUMN is_super_admin;
ALTER TABLE users DROP COLUMN password_hash;
