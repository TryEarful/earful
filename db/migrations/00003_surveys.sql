-- +goose Up

-- M3: surveys, drafts, revisions, published versions, questions.
--
-- Two invariants are enforced here in the database rather than only in Go,
-- because they are the product's central promises and must hold against
-- every path -- including a future admin script or a psql session:
--   * ADR-0001: published versions and their questions are immutable.
--   * ADR-0003: a survey's anonymity is fixed at creation, forever.

CREATE TABLE surveys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id),
    title        text NOT NULL,
    -- Immutable after insert (trigger below). ADR-0003: the promise made
    -- to respondents cannot be revised later.
    is_anonymous boolean NOT NULL,
    -- Survey Status (Draft|Open|Closed) is derived, never stored: no
    -- published version = Draft; closed_at or a past close_at = Closed;
    -- otherwise Open. Storing it would allow the stored value and the
    -- underlying facts to disagree.
    close_at     timestamptz,
    closed_at    timestamptz,
    created_by   uuid NOT NULL REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

CREATE INDEX surveys_workspace_idx ON surveys (workspace_id) WHERE deleted_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION surveys_guard_anonymity() RETURNS trigger AS $$
BEGIN
    IF NEW.is_anonymous IS DISTINCT FROM OLD.is_anonymous THEN
        RAISE EXCEPTION 'surveys.is_anonymous is immutable (ADR-0003): survey %', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER surveys_guard_anonymity
    BEFORE UPDATE ON surveys
    FOR EACH ROW EXECUTE FUNCTION surveys_guard_anonymity();

-- Question Identity: the stable thread a question keeps across versions.
-- Rewording preserves it; a new question gets a new one; deleting a
-- question simply ends it (no row is removed -- responses still reference
-- it).
CREATE TABLE question_identities (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    survey_id  uuid NOT NULL REFERENCES surveys (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX question_identities_survey_idx ON question_identities (survey_id);

-- The single mutable working copy. structure holds the question list as
-- jsonb: it is edited as a whole document, never queried field-by-field,
-- and freezing it into relational rows at publish is exactly what makes
-- published versions immutable.
CREATE TABLE survey_drafts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    survey_id  uuid NOT NULL UNIQUE REFERENCES surveys (id),
    structure  jsonb NOT NULL DEFAULT '{"questions":[]}'::jsonb,
    updated_by uuid REFERENCES users (id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Append-only autosave history; the Audit Log view is derived from this
-- plus publishes.
CREATE TABLE draft_revisions (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    draft_id  uuid NOT NULL REFERENCES survey_drafts (id),
    structure jsonb NOT NULL,
    saved_by  uuid REFERENCES users (id),
    saved_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX draft_revisions_draft_idx ON draft_revisions (draft_id, saved_at DESC);

CREATE TABLE survey_versions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    survey_id    uuid NOT NULL REFERENCES surveys (id),
    number       integer NOT NULL,
    published_by uuid REFERENCES users (id),
    published_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (survey_id, number)
);

CREATE TABLE questions (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id           uuid NOT NULL REFERENCES survey_versions (id),
    question_identity_id uuid NOT NULL REFERENCES question_identities (id),
    type                 text NOT NULL,
    text                 text NOT NULL,
    -- Options for choice/dropdown types; scale bounds and labels for
    -- rating/NPS. Empty for text and yes/no.
    options              jsonb NOT NULL DEFAULT '[]'::jsonb,
    required             boolean NOT NULL DEFAULT false,
    position             integer NOT NULL,
    UNIQUE (version_id, position),
    UNIQUE (version_id, question_identity_id),
    CONSTRAINT questions_type_supported CHECK (type IN (
        'long_text', 'short_text', 'single_choice', 'multiple_choice',
        'rating_scale', 'nps', 'yes_no', 'dropdown'
    ))
);

CREATE INDEX questions_version_idx ON questions (version_id, position);
CREATE INDEX questions_identity_idx ON questions (question_identity_id);

-- ADR-0001 enforcement: a published version and its questions are
-- immutable. The purge job (M8-T2) is the sole legitimate deleter, and it
-- announces itself by setting earful.purging -- a session-local setting
-- no ordinary request path ever sets.
-- +goose StatementBegin
CREATE FUNCTION reject_mutation_of_published() RETURNS trigger AS $$
BEGIN
    IF current_setting('earful.purging', true) = 'on' AND TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'published survey data is immutable (ADR-0001): % on %',
        TG_OP, TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER survey_versions_immutable
    BEFORE UPDATE OR DELETE ON survey_versions
    FOR EACH ROW EXECUTE FUNCTION reject_mutation_of_published();

CREATE TRIGGER questions_immutable
    BEFORE UPDATE OR DELETE ON questions
    FOR EACH ROW EXECUTE FUNCTION reject_mutation_of_published();

-- Draft revisions are append-only for the same reason the audit log is
-- trustworthy: nobody can rewrite what was saved.
CREATE TRIGGER draft_revisions_append_only
    BEFORE UPDATE OR DELETE ON draft_revisions
    FOR EACH ROW EXECUTE FUNCTION reject_mutation_of_published();

-- +goose Down
DROP TRIGGER draft_revisions_append_only ON draft_revisions;
DROP TRIGGER questions_immutable ON questions;
DROP TRIGGER survey_versions_immutable ON survey_versions;
DROP FUNCTION reject_mutation_of_published();
DROP TRIGGER surveys_guard_anonymity ON surveys;
DROP FUNCTION surveys_guard_anonymity();
DROP TABLE questions;
DROP TABLE survey_versions;
DROP TABLE draft_revisions;
DROP TABLE survey_drafts;
DROP TABLE question_identities;
DROP TABLE surveys;
