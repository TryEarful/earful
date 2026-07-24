-- +goose Up

-- M4: participants, responses, answers.
--
-- ADR-0003 is visible in what is ABSENT here: the response path has no ip
-- column, no user_agent column, no fingerprint of any kind. For an
-- anonymous survey participant_id stays NULL forever, so there is no
-- column on this path that could identify a respondent even if someone
-- wanted it to. IP addresses exist only in abuse_log (added with the
-- anti-abuse layer), which carries no foreign key to responses.

CREATE TABLE participants (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    survey_id   uuid NOT NULL REFERENCES surveys (id),
    email       text NOT NULL,
    -- Only the hash of the invite token is stored, so a database leak
    -- cannot be replayed into someone else's survey response.
    token_hash  bytea NOT NULL UNIQUE,
    invited_at  timestamptz,
    bounced_at  timestamptz,
    submitted_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    UNIQUE (survey_id, email)
);

CREATE INDEX participants_survey_idx ON participants (survey_id) WHERE deleted_at IS NULL;

CREATE TABLE responses (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    survey_id     uuid NOT NULL REFERENCES surveys (id),
    -- The version the respondent was actually served (ADR-0001). Never
    -- rewritten when a newer version is published: the answers belong to
    -- the questions as they were asked.
    version_id    uuid NOT NULL REFERENCES survey_versions (id),
    -- NULL forever for anonymous surveys (ADR-0003).
    participant_id uuid REFERENCES participants (id),
    duration_secs integer,
    submitted_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

CREATE INDEX responses_survey_idx ON responses (survey_id) WHERE deleted_at IS NULL;
CREATE INDEX responses_version_idx ON responses (version_id);

-- One participant answers once (M4-T3). Partial so anonymous responses,
-- which all carry NULL, are unconstrained.
CREATE UNIQUE INDEX responses_one_per_participant
    ON responses (participant_id)
    WHERE participant_id IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE answers (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    response_id          uuid NOT NULL REFERENCES responses (id),
    question_id          uuid NOT NULL REFERENCES questions (id),
    -- Denormalised on purpose: results aggregate by Question Identity
    -- across versions (ADR-0001), and carrying it here keeps that read
    -- path from joining through questions on every row.
    question_identity_id uuid NOT NULL REFERENCES question_identities (id),
    value                jsonb NOT NULL,
    UNIQUE (response_id, question_id)
);

CREATE INDEX answers_response_idx ON answers (response_id);
CREATE INDEX answers_identity_idx ON answers (question_identity_id);

-- A submitted answer is as immutable as the question it answers: there is
-- no response editing in the MVP (CONTEXT.md: "Responses themselves are
-- final at submission"). Deletion remains possible for the purge job.
CREATE TRIGGER answers_immutable
    BEFORE UPDATE ON answers
    FOR EACH ROW EXECUTE FUNCTION reject_mutation_of_published();

-- +goose Down
DROP TRIGGER answers_immutable ON answers;
DROP TABLE answers;
DROP TABLE responses;
DROP TABLE participants;
