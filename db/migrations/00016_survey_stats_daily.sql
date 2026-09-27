-- Daily survey flow counters (ADR-0012, amending ADR-0009).
--
-- survey_stats (00011) holds running totals with no time dimension, which
-- is enough for "how is this survey going" and not enough for "how did it
-- go this week". This table gives the three flow metrics — opened,
-- submitted, and the question where answers stopped — a day, and only
-- those three.
--
-- The audience metrics (browser, device, country) stay in survey_stats
-- as undated totals on purpose. A per-day audience count plus a date
-- picker would let a creator subtract two ranges and read off one day's
-- countries, and on a small anonymous survey one day is one person. The
-- suppression rule protects a single query; nothing protects a pair, so
-- the dimension is simply not recorded for those metrics.
--
-- Everything ADR-0009 says about survey_stats holds here too: no FK to
-- responses, no response id, and the same build-time tests guard it (the
-- table name contains "survey_stats" so the query scan already covers
-- it). The day is the UTC date of the request.
--
-- `reached` is keyed by Question Identity rather than by position, so a
-- question inserted in a later version does not shift every earlier
-- count onto the wrong row. The rows already in survey_stats keep their
-- position key and are reported as the undated total they are.

-- +goose Up
CREATE TABLE survey_stats_daily (
    survey_id uuid   NOT NULL REFERENCES surveys (id),
    metric    text   NOT NULL,
    bucket    text   NOT NULL DEFAULT '',
    day       date   NOT NULL,
    count     bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (survey_id, metric, bucket, day),
    CONSTRAINT survey_stats_daily_metric_blessed CHECK (metric IN (
        'start', 'completion', 'reached'
    ))
);

COMMENT ON TABLE survey_stats_daily IS
    'Unlinked per-day survey flow counters (ADR-0012). No join path to responses exists or may be added.';

-- +goose Down
DROP TABLE survey_stats_daily;
