-- The pictures of a survey's style (ADR-0018): its logo and its banner.
--
-- They are stored here, beside the survey, and served from Earful's own
-- origin: a respondent's page loads nothing from anywhere else
-- (ADR-0006), and an instance started with docker compose has the
-- feature with nothing more to run (ADR-0010). What is stored is never
-- the uploaded file: internal/styleimage decodes it and encodes it
-- again, so a row holds pixels and no metadata.
--
-- A picture belongs to one survey and is found by the hash of its bytes,
-- which is also its address (/style-image/{sha256}). A style refers to
-- it by that hash, in the draft's jsonb and in survey_versions.style.
-- The same upload twice is one row.
--
-- A row is never changed: its bytes are what its address promises, and a
-- browser is told it may keep them for good. It is not deleted while a
-- published version of its survey refers to it, because the look of the
-- page is part of what a respondent was shown (ADR-0001). The trigger
-- below holds both against any path; the purge job, which erases a
-- survey whole, announces itself as it does to the other guards.

-- +goose Up
CREATE TABLE survey_images (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    survey_id    uuid NOT NULL REFERENCES surveys (id),
    sha256       bytea NOT NULL CHECK (octet_length(sha256) = 32),
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg')),
    width        int NOT NULL CHECK (width > 0),
    height       int NOT NULL CHECK (height > 0),
    size_bytes   int NOT NULL CHECK (size_bytes > 0),
    bytes        bytea NOT NULL,
    created_at   timestamptz NOT NULL,
    UNIQUE (survey_id, sha256)
);

-- The public address names a picture by its hash alone.
CREATE INDEX survey_images_sha256_idx ON survey_images (sha256);

-- +goose StatementBegin
CREATE FUNCTION survey_images_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'survey_images rows are immutable: image %', OLD.id;
    END IF;
    IF current_setting('earful.purging', true) = 'on' THEN
        RETURN OLD;
    END IF;
    IF EXISTS (
        SELECT 1 FROM survey_versions v
        WHERE v.survey_id = OLD.survey_id
          AND v.style IS NOT NULL
          AND jsonb_path_exists(v.style, '$.** ? (@.sha256 == $hash)',
                                jsonb_build_object('hash', encode(OLD.sha256, 'hex')))
    ) THEN
        RAISE EXCEPTION 'a published version shows image % (ADR-0001)', OLD.id;
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER survey_images_guard
    BEFORE UPDATE OR DELETE ON survey_images
    FOR EACH ROW EXECUTE FUNCTION survey_images_guard();

-- +goose Down
-- Fails while any version shows a picture, rather than changing what
-- respondents were shown (ADR-0001).
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM survey_versions
        WHERE style IS NOT NULL AND jsonb_path_exists(style, '$.**.sha256')
    ) THEN
        RAISE EXCEPTION 'published styles show images (ADR-0001)';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER survey_images_guard ON survey_images;
DROP FUNCTION survey_images_guard();
DROP TABLE survey_images;
