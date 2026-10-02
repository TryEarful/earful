-- A survey's style: how its pages look to the people answering it
-- (ADR-0018). For now that is the theme the pages are drawn in.
--
-- It belongs to the published version rather than to the survey: the
-- look of the page is part of what a respondent was shown, and a
-- version is immutable (ADR-0001). Choosing a theme is a draft change
-- like any other, and reaches respondents only with the next publish.
-- The column sits on survey_versions, so the trigger that already
-- refuses UPDATE and DELETE there guards it too.
--
-- NULL is "no style", and the pages are then drawn as every page of
-- Earful is: every version published before this migration keeps
-- exactly what it showed.
--
-- The value is the draft's style object as it was at publish, such as
-- {"theme": "ocean"}. It is read whole with the version row and never
-- queried field by field, the same reasoning as the draft's jsonb.
--
-- Down fails while any version carries a style, rather than changing
-- what respondents were shown (ADR-0001).

-- +goose Up
ALTER TABLE survey_versions
    ADD COLUMN style jsonb;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM survey_versions WHERE style IS NOT NULL) THEN
        RAISE EXCEPTION 'published styles exist (ADR-0001)';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE survey_versions
    DROP COLUMN style;
