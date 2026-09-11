-- +goose Up
-- Phase 1: the raw fact table. One row per proxied request (Part E.1).
-- Money is bigint micro-USD throughout — never float.
--
-- Partitioned by RANGE(started_at) per the specification. Automated
-- per-month partition creation is an operational concern of a later
-- phase's retention/partition-management job; Phase 1 creates a single
-- DEFAULT partition so the table is immediately usable without that job
-- existing yet. A later migration can split the default partition into
-- real per-month partitions without changing this table's shape.

CREATE TABLE requests (
    id       uuid NOT NULL,
    org_id   uuid NOT NULL,
    app_id   uuid NOT NULL,
    api_key_id uuid,

    started_at   timestamptz NOT NULL,
    duration_ms  integer NOT NULL,
    ttft_ms      integer,

    endpoint  text NOT NULL,
    protocol  text NOT NULL,
    streamed  boolean NOT NULL,

    requested_model text NOT NULL,
    provider         text NOT NULL,
    model            text NOT NULL,
    route_reason     text NOT NULL DEFAULT 'direct',
    route_variant    text,
    policy_version   integer NOT NULL DEFAULT 0,

    input_tokens        integer NOT NULL DEFAULT 0,
    output_tokens        integer NOT NULL DEFAULT 0,
    cached_input_tokens   integer NOT NULL DEFAULT 0,
    total_tokens           integer NOT NULL DEFAULT 0,
    usage_source            text NOT NULL DEFAULT 'provider',

    cost_micro         bigint NOT NULL DEFAULT 0,
    cost_input_micro   bigint NOT NULL DEFAULT 0,
    cost_output_micro  bigint NOT NULL DEFAULT 0,
    cost_status        text NOT NULL DEFAULT 'unknown' CHECK (cost_status IN ('known', 'unknown', 'local')),
    pricing_version    text NOT NULL DEFAULT '',

    cache_status      text NOT NULL DEFAULT 'disabled' CHECK (cache_status IN ('hit', 'miss', 'bypass', 'disabled')),
    cache_key         bytea,
    cache_saved_micro bigint NOT NULL DEFAULT 0,

    status        text NOT NULL,
    http_status   integer,
    error_code    text,
    error_message text,

    has_tools          boolean NOT NULL DEFAULT false,
    has_tool_calls     boolean NOT NULL DEFAULT false,
    has_images         boolean NOT NULL DEFAULT false,
    json_mode          boolean NOT NULL DEFAULT false,
    temperature        real,
    max_tokens         integer,
    message_count      smallint,
    system_prompt_hash bytea,

    request_body  jsonb,
    response_body jsonb,

    PRIMARY KEY (started_at, id)
) PARTITION BY RANGE (started_at);

CREATE TABLE requests_default PARTITION OF requests DEFAULT;

CREATE INDEX requests_app_started_idx ON requests (app_id, started_at DESC);
CREATE INDEX requests_app_model_started_idx ON requests (app_id, model, started_at DESC);
CREATE INDEX requests_app_cache_key_started_idx ON requests (app_id, cache_key, started_at DESC) WHERE cache_key IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS requests;
