-- +goose Up
-- Phase 1: applications are the fundamental unit of Fluxen (Part E.1).
-- api_keys is the attribution mechanism — a key identifies exactly one
-- application.

CREATE TABLE applications (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    slug          text NOT NULL,
    name          text NOT NULL,
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'archived')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    first_seen_at timestamptz,
    last_seen_at  timestamptz,
    UNIQUE (org_id, slug)
);

CREATE TABLE api_keys (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id     uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    name       text NOT NULL,
    prefix     text NOT NULL,
    key_hash   text NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Only one active key may ever own a given prefix; a revoked key's prefix
-- becomes free again since a new key issuance regenerates a fresh random
-- prefix anyway (collision is astronomically unlikely either way, but the
-- partial index keeps the invariant explicit).
CREATE UNIQUE INDEX api_keys_prefix_active_idx ON api_keys (prefix) WHERE revoked_at IS NULL;
CREATE INDEX api_keys_app_id_idx ON api_keys (app_id);

-- +goose Down
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS applications;
