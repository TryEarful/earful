-- The account's style (ADR-0023): the theme, header, footer and thanks
-- picture that every survey of a workspace follows until it makes a part
-- its own.
--
-- workspace_styles holds one style per workspace, and its words in each
-- language they have been translated into. A workspace with no row has
-- no account style: its surveys are drawn as they always were. The style
-- is never shown to a respondent from here. A survey's published version
-- freezes the style it resolved to (ADR-0018), so a change made here
-- reaches respondents only when a survey is published again.
--
-- workspace_images holds the account style's pictures, as survey_images
-- holds a survey's: re-encoded by internal/styleimage, found by the hash
-- of their bytes, never changed once stored. A survey that shows one
-- copies it into its own survey_images when it is saved or published,
-- so a version only ever refers to its survey's pictures and nothing
-- here needs to be kept for a respondent's sake. A picture nothing in the
-- account's style refers to is removed by the purge after a week.

-- +goose Up
CREATE TABLE workspace_styles (
    workspace_id  uuid PRIMARY KEY REFERENCES workspaces (id),
    style         jsonb NOT NULL DEFAULT '{}',
    localizations jsonb NOT NULL DEFAULT '{}',
    updated_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL
);

CREATE TABLE workspace_images (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    sha256       bytea NOT NULL CHECK (octet_length(sha256) = 32),
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg')),
    width        int NOT NULL CHECK (width > 0),
    height       int NOT NULL CHECK (height > 0),
    size_bytes   int NOT NULL CHECK (size_bytes > 0),
    bytes        bytea NOT NULL,
    created_at   timestamptz NOT NULL,
    UNIQUE (workspace_id, sha256)
);

-- A row's bytes are what its address promises, so it is never changed.
-- Deleting one needs no guard: no published version refers to it.
-- +goose StatementBegin
CREATE FUNCTION workspace_images_guard() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'workspace_images rows are immutable: image %', OLD.id;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER workspace_images_guard
    BEFORE UPDATE ON workspace_images
    FOR EACH ROW EXECUTE FUNCTION workspace_images_guard();

-- +goose Down
DROP TRIGGER workspace_images_guard ON workspace_images;
DROP FUNCTION workspace_images_guard();
DROP TABLE workspace_images;
DROP TABLE workspace_styles;
