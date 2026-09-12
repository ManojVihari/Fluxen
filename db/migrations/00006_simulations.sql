-- +goose Up
-- Phase 4: a scenario run against real historical request facts (Part
-- E.1). Append-only — a simulation is a record of "what if," never
-- mutated after creation.

CREATE TABLE simulations (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL,
    app_id         uuid NOT NULL,
    opportunity_id uuid REFERENCES opportunities(id) ON DELETE SET NULL,

    scenario jsonb NOT NULL,

    window_start timestamptz NOT NULL,
    window_end   timestamptz NOT NULL,

    replayed_requests bigint NOT NULL,
    affected_requests bigint NOT NULL,
    sampled            boolean NOT NULL DEFAULT false,

    actual_cost_micro               bigint NOT NULL,
    simulated_cost_micro            bigint NOT NULL,
    delta_micro                     bigint NOT NULL,
    delta_pct                       double precision NOT NULL,
    projected_monthly_savings_micro bigint NOT NULL,

    breakdown   jsonb NOT NULL,
    assumptions jsonb NOT NULL,

    engine_version text NOT NULL,
    created_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX simulations_app_created_idx ON simulations (app_id, created_at DESC);
CREATE INDEX simulations_opportunity_idx ON simulations (opportunity_id) WHERE opportunity_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS simulations;
