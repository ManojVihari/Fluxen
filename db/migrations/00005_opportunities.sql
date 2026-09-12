-- +goose Up
-- Phase 3: the product's primary noun (Part E.1). Detector output only —
-- simulation/apply/measurement columns referenced elsewhere in Part E
-- live on their own tables (simulations, measurements, policy_history)
-- that arrive with the phases that write them. The full opportunity shape
-- is created now, not incrementally, because Phase 5's apply flow will
-- read `recommendation` as-is (Part L Phase 3 database tasks).

CREATE TABLE opportunities (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id   uuid NOT NULL,
    app_id   uuid NOT NULL,

    kind        text NOT NULL CHECK (kind IN ('model_cost', 'repeated_request', 'token_efficiency', 'traffic_anomaly')),
    fingerprint text NOT NULL,
    status      text NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'reviewed', 'simulated', 'applied', 'measured', 'dismissed', 'stale', 'reverted', 'failed')),
    severity    text CHECK (severity IN ('medium', 'high')),

    title   text NOT NULL,
    summary text NOT NULL,

    window_start    timestamptz NOT NULL,
    window_end      timestamptz NOT NULL,
    sample_requests bigint NOT NULL,

    current_cost_micro   bigint NOT NULL,
    projected_cost_micro bigint NOT NULL,
    savings_micro        bigint NOT NULL,
    savings_pct          double precision NOT NULL,

    confidence       text NOT NULL CHECK (confidence IN ('low', 'medium', 'high')),
    confidence_score double precision NOT NULL,

    evidence       jsonb NOT NULL,
    recommendation jsonb NOT NULL,

    detector_version text NOT NULL,
    detected_at      timestamptz NOT NULL DEFAULT now(),
    reviewed_at      timestamptz,
    reviewed_by      uuid,
    dismissed_at     timestamptz,
    dismiss_reason   text,
    last_seen_at     timestamptz NOT NULL DEFAULT now()
);

-- A detector re-run against unchanged traffic must not create a
-- duplicate — the partial unique index only covers the "live" states, so
-- a dismissed/stale opportunity's fingerprint can legitimately re-open as
-- a fresh row later (Part G.1).
CREATE UNIQUE INDEX opportunities_app_fingerprint_open_idx
    ON opportunities (app_id, fingerprint)
    WHERE status IN ('open', 'reviewed', 'simulated');

CREATE INDEX opportunities_app_status_idx ON opportunities (app_id, status);
CREATE INDEX opportunities_org_status_idx ON opportunities (org_id, status);

-- +goose Down
DROP TABLE IF EXISTS opportunities;
