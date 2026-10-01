-- The number question type: a whole number between bounds the author
-- sets, answered with the browser's own number field and stored in the
-- answer's jsonb as {"number": n}. The bounds live in scale_min and
-- scale_max (00009), frozen at publish exactly as a rating's scale is.
--
-- The set of question types is closed, and this CHECK is the database's
-- copy of it (see domain.QuestionTypes). A type the application knows and
-- the constraint does not would fail at publish, not in the editor.
--
-- Down cannot restore the narrower constraint while a published question
-- of type 'number' exists; it fails rather than deleting published data
-- (ADR-0001).

-- +goose Up
ALTER TABLE questions DROP CONSTRAINT questions_type_supported;
ALTER TABLE questions ADD CONSTRAINT questions_type_supported CHECK (type IN (
    'long_text', 'short_text', 'single_choice', 'multiple_choice',
    'rating_scale', 'nps', 'yes_no', 'dropdown', 'date', 'number'
));

-- +goose Down
ALTER TABLE questions DROP CONSTRAINT questions_type_supported;
ALTER TABLE questions ADD CONSTRAINT questions_type_supported CHECK (type IN (
    'long_text', 'short_text', 'single_choice', 'multiple_choice',
    'rating_scale', 'nps', 'yes_no', 'dropdown', 'date'
));
