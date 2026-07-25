-- M11-T1: question Localizations, frozen at publish.
--
-- A Localization is part of a Survey Version, not a decoration on top of
-- one: what a respondent saw in their language has to be as immutable as
-- what an English-speaking respondent saw (ADR-0001 applied to
-- translations). So these rows are written in the publish transaction
-- and rejected for UPDATE and DELETE by the same trigger that guards
-- questions — adding a language later means publishing a new version.
--
-- Drafting and review happen in the draft's JSON, where editing is
-- ordinary editing and every save appends a revision. Only reviewed
-- languages reach this table.

-- +goose Up
CREATE TABLE question_localizations (
    version_id  uuid NOT NULL REFERENCES survey_versions (id),
    question_id uuid NOT NULL REFERENCES questions (id),
    -- BCP-47-ish language subtag: "nl", "pt-BR".
    lang        text NOT NULL,
    text        text NOT NULL,
    options     jsonb NOT NULL DEFAULT '[]'::jsonb,
    PRIMARY KEY (question_id, lang)
);

CREATE INDEX question_localizations_version_idx ON question_localizations (version_id, lang);

CREATE TRIGGER question_localizations_immutable
    BEFORE UPDATE OR DELETE ON question_localizations
    FOR EACH ROW EXECUTE FUNCTION reject_mutation_of_published();

-- +goose Down
DROP TRIGGER question_localizations_immutable ON question_localizations;
DROP TABLE question_localizations;
