-- M7-T4: survey stats and audience aggregates (ADR-0009).
--
-- These counters are the whole of what Earful knows about who answers a
-- survey, and they are deliberately shaped so that knowledge cannot be
-- narrowed to a person:
--
--   * There is no FK to responses, no response_id, no timestamp finer
--     than the counter itself. A row says "42 answers came from Chrome",
--     never "this answer came from Chrome". An automated test asserts
--     that no query in db/queries mentions this table together with
--     responses or answers.
--   * Country is derived in-process from an embedded database and the IP
--     is discarded in the same request (internal/geoip). No lookup
--     service, no new sub-processor.
--   * The user agent is parsed to a family and a device class in-request
--     and never stored.
--   * The UI suppresses any bucket below five observations.
--
-- The blessed metric list is exhaustive (ADR-0009): start, completion,
-- last answered position, browser, device, country. Anything else needs
-- a new ADR, not a new row.
--
-- Per-response duration is the one per-response addition the ADR allows,
-- and it already exists as responses.duration_secs — averages come from
-- there, not from here.

-- +goose Up
CREATE TABLE survey_stats (
    survey_id uuid   NOT NULL REFERENCES surveys (id),
    metric    text   NOT NULL,
    bucket    text   NOT NULL DEFAULT '',
    count     bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (survey_id, metric, bucket),
    CONSTRAINT survey_stats_metric_blessed CHECK (metric IN (
        'start', 'completion', 'reached', 'browser', 'device', 'country'
    ))
);

COMMENT ON TABLE survey_stats IS
    'Unlinked survey-level counters (ADR-0009). No join path to responses exists or may be added.';

-- +goose Down
DROP TABLE survey_stats;
