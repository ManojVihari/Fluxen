-- +goose Up
-- Phase 7: the three retention knobs Settings > Retention exposes (Part
-- E.2/I.6). V1 is single-org per deployment, so these live directly on
-- organizations rather than a separate settings table — one row, one
-- set of knobs, no ambiguity about which one applies.

ALTER TABLE organizations
    ADD COLUMN requests_retention_days integer NOT NULL DEFAULT 90,
    ADD COLUMN body_retention_days     integer NOT NULL DEFAULT 7,
    ADD COLUMN body_capture_enabled    boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE organizations
    DROP COLUMN requests_retention_days,
    DROP COLUMN body_retention_days,
    DROP COLUMN body_capture_enabled;
