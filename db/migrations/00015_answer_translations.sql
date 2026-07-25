-- M11-T2: creator-side answer translation.
--
-- The original answer is never touched: a translation is a separate row,
-- cached per (answer, language), labelled with the model that produced
-- it. A creator reading a global audience sees what people said in their
-- own words and, beside it, a machine translation clearly marked as one
-- (stories 26 and 27).
--
-- Unlike localizations, these are not frozen: a better model may
-- retranslate later, and nothing about the record of what a respondent
-- said changes when it does — because that record is in `answers`.

-- +goose Up
CREATE TABLE answer_translations (
    answer_id  uuid NOT NULL REFERENCES answers (id),
    lang       text NOT NULL,
    text       text NOT NULL,
    model      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (answer_id, lang)
);

-- +goose Down
DROP TABLE answer_translations;
