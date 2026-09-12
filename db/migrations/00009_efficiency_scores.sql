-- +goose Up
-- Phase 7: the daily Efficiency Score snapshot (PRD §20, Implementation
-- Plan Phase 7 backend tasks). internal/score's Runner computes and
-- upserts one row per (app_id, day) — idempotent, same convention as
-- Phase 2's rollups (Part C.7: "compute efficiency score snapshot,
-- daily").

CREATE TABLE efficiency_scores (
    app_id  uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    day     date NOT NULL,

    -- 'ok' once a real score was computed; 'insufficient_data' when the
    -- application doesn't yet clear the same traffic/spend floor the
    -- detectors use (Part G.2) -- overall/components are 0 in that case,
    -- never a fabricated number (Part D.2's "never fabricate" principle
    -- applied to this feature).
    status  text NOT NULL CHECK (status IN ('ok', 'insufficient_data')),

    overall integer NOT NULL,

    model_efficiency    integer NOT NULL,
    token_efficiency    integer NOT NULL,
    cache_efficiency    integer NOT NULL,
    traffic_stability   integer NOT NULL,
    cost_efficiency     integer NOT NULL,

    computed_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (app_id, day)
);

CREATE INDEX efficiency_scores_app_day_idx ON efficiency_scores (app_id, day DESC);

-- +goose Down
DROP TABLE IF EXISTS efficiency_scores;
