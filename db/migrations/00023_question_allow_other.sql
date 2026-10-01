-- An "Other" choice with a box to write in, offered on a single choice,
-- dropdown or multiple choice question when its creator asks for it.
--
-- The flag belongs to the published question, like required: whether a
-- respondent could write their own answer is part of what they were
-- shown, and a published question is immutable (ADR-0001). The column
-- sits on questions, so the trigger that already refuses UPDATE and
-- DELETE there guards it too. In a draft it travels in the structure's
-- jsonb with the rest of the question and is frozen here at publish.
--
-- The word "Other" is the interface's, from the message files, and is
-- shown in the respondent's language; question_localizations therefore
-- needs nothing new. What a respondent writes in the box is stored in
-- the answer's jsonb, beside the choice.
--
-- false is what every question published before this migration showed.
--
-- Down fails while any published question offers Other, rather than
-- erasing what respondents were shown (ADR-0001).

-- +goose Up
ALTER TABLE questions ADD COLUMN allow_other boolean NOT NULL DEFAULT false;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM questions WHERE allow_other) THEN
        RAISE EXCEPTION 'published questions offer Other (ADR-0001)';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE questions DROP COLUMN allow_other;
