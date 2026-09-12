-- +goose Up
-- Phase 5: the per-application control surface (Part E.1). `policies`
-- holds exactly the current document per app (mutated in place,
-- versioned); `policy_history` is the append-only audit trail every
-- mutation writes to, whatever the source.

CREATE TABLE policies (
    app_id     uuid PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    version    integer NOT NULL DEFAULT 0,
    document   jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid REFERENCES users(id) ON DELETE SET NULL
);

CREATE TABLE policy_history (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         uuid NOT NULL,
    version        integer NOT NULL,
    document       jsonb NOT NULL,
    diff           jsonb NOT NULL,
    change_source  text NOT NULL CHECK (change_source IN ('user', 'opportunity', 'revert')),
    opportunity_id uuid REFERENCES opportunities(id) ON DELETE SET NULL,
    simulation_id  uuid REFERENCES simulations(id) ON DELETE SET NULL,
    note           text,
    changed_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    changed_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX policy_history_app_version_idx ON policy_history (app_id, version DESC);

-- +goose Down
DROP TABLE IF EXISTS policy_history;
DROP TABLE IF EXISTS policies;
