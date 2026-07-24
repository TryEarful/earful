-- +goose Up

-- M4-T5: the abuse log. This is the ONLY place an IP address is ever
-- written (ADR-0003/ADR-0009), and it is structurally quarantined: no
-- foreign key to anything, no survey id, no join path that could tie an
-- IP to a response. Rows describe rejected requests (rate limits,
-- honeypots, failed challenges) for operational visibility, and the purge
-- job (M8-T2) trims them after 30 days.
CREATE TABLE abuse_log (
    id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ip   text NOT NULL,
    path text NOT NULL,
    kind text NOT NULL,
    at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX abuse_log_at_idx ON abuse_log (at);

-- +goose Down
DROP TABLE abuse_log;
