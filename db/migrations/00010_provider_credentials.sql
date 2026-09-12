-- +goose Up
-- Phase 7: org-scoped, encrypted provider credentials (Part E.1) —
-- replaces the deployment-wide env-var credentials (OPENAI_API_KEY,
-- GEMINI_API_KEY, OLLAMA_BASE_URL) that earlier phases used as a
-- documented simplification. api_key_encrypted is the AES-256-GCM
-- ciphertext (internal/crypto), never a plaintext key — nothing in this
-- table or any API response ever carries a decrypted key back out (Part
-- G's security pass: "keys never returned by any API").

CREATE TABLE provider_credentials (
    id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL,

    provider text NOT NULL CHECK (provider IN ('openai', 'gemini', 'ollama')),

    -- Encrypted API key (openai/gemini) — bytea ciphertext, nullable
    -- since a stock Ollama install often has no key at all.
    api_key_encrypted bytea,
    -- Base URL override (Ollama's endpoint, or an OpenAI-compatible
    -- proxy) — plaintext; a base URL is not a secret.
    base_url text,

    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,

    last_health_check_at     timestamptz,
    last_health_check_status text CHECK (last_health_check_status IN ('ok', 'error')),
    last_health_check_error  text
);

-- One active credential per (org, provider) at a time — V1's single-org
-- reality means there is never an ambiguous "which one applies" question
-- the gateway would otherwise have to resolve.
CREATE UNIQUE INDEX provider_credentials_org_provider_active_idx
    ON provider_credentials (org_id, provider) WHERE status = 'active';

CREATE INDEX provider_credentials_org_idx ON provider_credentials (org_id);

-- +goose Down
DROP TABLE IF EXISTS provider_credentials;
