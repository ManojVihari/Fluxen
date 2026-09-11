-- +goose Up
-- Phase 0: schema only for organizations and users (Part E.1 of the
-- implementation specification). No application, request, or product
-- table is created yet — those arrive with the phases that need them.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE organizations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email         citext NOT NULL,
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('owner', 'member')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, email)
);

-- +goose Down
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
