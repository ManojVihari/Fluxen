-- +goose Up
-- Phase 6: the before/after outcome of an applied opportunity (Part
-- E.1). One row per apply — created at apply time with the baseline
-- already frozen, then updated in place as interim/final checks run.

CREATE TABLE measurements (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL,
    app_id         uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    opportunity_id uuid NOT NULL REFERENCES opportunities(id) ON DELETE CASCADE,
    simulation_id  uuid REFERENCES simulations(id) ON DELETE SET NULL,
    policy_version integer NOT NULL,
    applied_at     timestamptz NOT NULL,

    baseline_start              timestamptz NOT NULL,
    baseline_end                timestamptz NOT NULL,
    baseline_requests           bigint NOT NULL,
    baseline_cost_micro         bigint NOT NULL,
    baseline_cost_per_1k_micro  bigint NOT NULL,

    observed_start              timestamptz,
    observed_end                timestamptz,
    observed_requests           bigint,
    observed_cost_micro         bigint,
    observed_cost_per_1k_micro  bigint,

    expected_savings_micro bigint NOT NULL,
    actual_savings_micro   bigint,
    expected_pct           double precision NOT NULL,
    actual_pct             double precision,

    verdict        text CHECK (verdict IN ('successful', 'partial', 'no_effect', 'regressed', 'inconclusive')),
    verdict_reason text,

    status       text NOT NULL DEFAULT 'collecting' CHECK (status IN ('collecting', 'interim', 'final', 'reverted')),
    finalized_at timestamptz
);

CREATE INDEX measurements_app_idx ON measurements (app_id, applied_at DESC);
CREATE INDEX measurements_opportunity_idx ON measurements (opportunity_id);
-- Only one live (non-reverted) measurement collects per opportunity at a
-- time — Apply is only reachable from open/reviewed/simulated (Part
-- G.1), so an opportunity can't be applied twice while already applied.
CREATE UNIQUE INDEX measurements_opportunity_live_idx ON measurements (opportunity_id) WHERE status != 'reverted';

-- Which measurements the scheduler still needs to check — partial index
-- keeps this cheap even with a long history of finalized rows.
CREATE INDEX measurements_pending_idx ON measurements (applied_at) WHERE status IN ('collecting', 'interim');

-- +goose Down
DROP TABLE IF EXISTS measurements;
