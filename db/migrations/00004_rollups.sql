-- +goose Up
-- Phase 2: aggregates for charts and the Applications/Application Detail
-- screens (Part E.1). Nothing in the UI queries `requests` directly for
-- these numbers — that keeps Application Detail fast regardless of how
-- much raw history has accumulated.
--
-- Both rollup tables are written exclusively by internal/rollup's
-- idempotent recompute (delete-then-insert a bucket range) — never
-- incrementally updated — so a job re-run for the same window can never
-- double-count.

CREATE TABLE request_rollup_hourly (
    org_id          uuid NOT NULL,
    app_id          uuid NOT NULL,
    bucket          timestamptz NOT NULL,
    provider        text NOT NULL,
    model           text NOT NULL,
    status          text NOT NULL,
    requests        bigint NOT NULL,
    input_tokens    bigint NOT NULL,
    output_tokens   bigint NOT NULL,
    total_tokens    bigint NOT NULL,
    cost_micro      bigint NOT NULL,
    duration_ms_sum bigint NOT NULL,
    PRIMARY KEY (app_id, bucket, provider, model, status)
);

CREATE INDEX request_rollup_hourly_bucket_idx ON request_rollup_hourly (bucket);

CREATE TABLE request_rollup_daily (
    org_id          uuid NOT NULL,
    app_id          uuid NOT NULL,
    day             date NOT NULL,
    provider        text NOT NULL,
    model           text NOT NULL,
    status          text NOT NULL,
    requests        bigint NOT NULL,
    input_tokens    bigint NOT NULL,
    output_tokens   bigint NOT NULL,
    total_tokens    bigint NOT NULL,
    cost_micro      bigint NOT NULL,
    duration_ms_sum bigint NOT NULL,
    PRIMARY KEY (app_id, day, provider, model, status)
);

CREATE INDEX request_rollup_daily_day_idx ON request_rollup_daily (day);

-- One row per app per day — the input to the Applications list table and,
-- from a later phase, the efficiency score.
CREATE TABLE application_daily (
    app_id          uuid NOT NULL,
    day             date NOT NULL,
    requests        bigint NOT NULL,
    errors          bigint NOT NULL,
    input_tokens    bigint NOT NULL,
    output_tokens   bigint NOT NULL,
    total_tokens    bigint NOT NULL,
    cost_micro      bigint NOT NULL,
    duration_ms_sum bigint NOT NULL,
    PRIMARY KEY (app_id, day)
);

-- +goose Down
DROP TABLE IF EXISTS application_daily;
DROP TABLE IF EXISTS request_rollup_daily;
DROP TABLE IF EXISTS request_rollup_hourly;
