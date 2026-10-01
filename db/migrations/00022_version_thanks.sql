-- A creator's own thank you message, and an optional link, shown to a
-- respondent after they send their answers.
--
-- They belong to the published version rather than to the survey: what a
-- respondent reads after answering is part of what they were shown, and
-- a version is immutable (ADR-0001). Editing them is a draft change like
-- any other, and reaches respondents only with the next publish. The
-- columns sit on survey_versions, so the trigger that already refuses
-- UPDATE and DELETE there guards them too.
--
-- NULL is "not set", and the page then shows the default text: every
-- version published before this migration keeps exactly what it showed.
--
-- thanks_localizations holds the message and link label per language,
-- as {"<lang>": {"message": "...", "link_label": "..."}}. It is read
-- whole with the version row and never queried field by field, the same
-- reasoning as the draft's jsonb. Only reviewed translations reach it.
--
-- Down fails while any version carries a message or a link, rather than
-- deleting what respondents were shown (ADR-0001).

-- +goose Up
ALTER TABLE survey_versions
    ADD COLUMN thanks_message       text,
    ADD COLUMN thanks_link_label    text,
    ADD COLUMN thanks_link_url      text,
    ADD COLUMN thanks_localizations jsonb;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM survey_versions
               WHERE thanks_message IS NOT NULL OR thanks_link_url IS NOT NULL) THEN
        RAISE EXCEPTION 'published thank you messages exist (ADR-0001)';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE survey_versions
    DROP COLUMN thanks_localizations,
    DROP COLUMN thanks_link_url,
    DROP COLUMN thanks_link_label,
    DROP COLUMN thanks_message;
