-- Defect fix (found while planning M7): published rating scales lost
-- their bounds.
--
-- domain.Question carries ScaleMin/ScaleMax in the draft JSON, but
-- Publish never wrote them and QuestionsForVersion never read them back,
-- so every published rating_scale question came back as 0..0: the
-- respondent renderer drew a single radio labelled "0" and the answer
-- validator accepted only 0. Preview reads the draft, which still has the
-- bounds, which is why the bug never showed in preview.
--
-- The bounds get their own columns rather than being smuggled into
-- `options` (which 00003's comment once imagined): they are numbers with
-- an invariant, not option strings, and M7-T1's distributions must be
-- able to enumerate the scale of the exact version a response was pinned
-- to (ADR-0001) — including versions whose wording, and scale, differ.
--
-- Nullable, because versions published before this migration genuinely do
-- not carry the information. domain.Question.Scale() falls back to the
-- editor's own defaults (1..5) for those rows, so old surveys render and
-- validate coherently instead of pretending to a precision we don't have.
-- NPS is untouched: its 0..10 is fixed by the metric's definition and is
-- never stored.

-- +goose Up
ALTER TABLE questions ADD COLUMN scale_min integer;
ALTER TABLE questions ADD COLUMN scale_max integer;

COMMENT ON COLUMN questions.options IS
    'Option strings for choice/dropdown types; empty for text, yes/no and scale types (see scale_min/scale_max).';

-- +goose Down
ALTER TABLE questions DROP COLUMN scale_max;
ALTER TABLE questions DROP COLUMN scale_min;
