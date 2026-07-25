-- M5-T4: voice quotas.
--
-- Transcription is billed by audio duration, not by the characters the
-- model returns, so the meter needs a second dimension. duration_secs is
-- zero for every text operation and the number of seconds of speech for
-- a transcription.
--
-- The per-survey daily cap is summed from this column: it survives
-- restarts and agrees across instances, which an in-memory counter would
-- not. The per-response and per-IP caps deliberately stay in memory —
-- keying them by anything durable would mean storing a fact about a
-- respondent, which anonymous surveys must not do (ADR-0003).

-- +goose Up
ALTER TABLE ai_usage ADD COLUMN duration_secs integer NOT NULL DEFAULT 0;

CREATE INDEX ai_usage_survey_day_idx ON ai_usage (survey_id, day);

-- +goose Down
DROP INDEX ai_usage_survey_day_idx;
ALTER TABLE ai_usage DROP COLUMN duration_secs;
