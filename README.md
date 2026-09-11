# Fluxen

**AI Traffic, Optimized.**

Fluxen is a self-hosted AI traffic gateway and optimization platform. See:

- [`Fluxen V1 — Product Requirements Document.md`](./Fluxen%20V1%20—%20Product%20Requirements%20Document.md) — what Fluxen is and why.
- [`Fluxen V1 — Implementation Plan.md`](./Fluxen%20V1%20—%20Implementation%20Plan.md) — the developer-ready implementation specification and phased build plan. This is the primary engineering reference; read it before changing code.

## Current status

**Phase 0 — Foundation** and **Phase 1 — First AI Request** are implemented.

- Repository structure, local dev loop, Postgres + Redis connectivity with migrations, structured logging, health/readiness/metrics endpoints.
- A control API (`/api/v1/...`) for first-run setup, login/logout, applications, and API keys.
- An OpenAI-compatible gateway (`/v1/chat/completions`) that authenticates by API key, proxies to real OpenAI (streaming and non-streaming), and persists every request — attributed, priced, and token-counted — to Postgres.
- A minimal dashboard: setup wizard, login, applications list/create, and a connect screen that issues an API key once with copy-paste Python/Node/curl snippets.

Not yet implemented (later phases, per the spec): any analytics/rollups or Application Detail view (Phase 2), Gemini/Ollama (Phase 7), caching/rate limits/budgets/model routing (Phase 5), and the optimization loop — detectors, simulation, apply, measure (Phases 3, 4, 5, 6).

## Quickstart (Docker Compose)

Requires Docker and Docker Compose.

```bash
docker compose up --build
```

This brings up four services:

| Service | Purpose | Host port |
|---|---|---|
| `postgres` | Primary datastore | 5433 → container 5432 (avoids colliding with a local Postgres) |
| `redis` | Cache / counters / sessions | 6379 |
| `fluxen` | Combined gateway + control binary | 8080 |
| `dashboard` | Next.js dashboard | 3000 |

Database migrations are applied automatically by the `fluxen` binary at startup.

Verify it's up:

```bash
curl http://localhost:8080/healthz   # liveness — process is serving
curl http://localhost:8080/readyz    # readiness — postgres + redis reachable
curl http://localhost:8080/metrics   # Prometheus metrics
```

Open the dashboard at [http://localhost:3000](http://localhost:3000) — it routes you to the setup wizard on first run.

### Connect an application

1. Open [http://localhost:3000](http://localhost:3000), complete the setup wizard (creates your organization and owner account), and you'll land on **Applications**.
2. Click **New application**, give it a name (e.g. "Document AI").
3. On the connect screen, click **Generate API key** — the raw key is shown exactly once, with ready-to-use Python, Node, and curl snippets.
4. Point your existing OpenAI SDK at Fluxen by changing only its `base_url` and `api_key`:

   ```python
   from openai import OpenAI

   client = OpenAI(base_url="http://localhost:8080/v1", api_key="fx_live_...")
   client.chat.completions.create(
       model="gpt-4o-mini",
       messages=[{"role": "user", "content": "Hello, Fluxen!"}],
   )
   ```

For the gateway to actually reach OpenAI, set `OPENAI_API_KEY` before starting the stack (see below) — without it, the gateway still runs, but every chat request returns `503 no_provider_credential`.

```bash
export OPENAI_API_KEY=sk-...
docker compose up --build
```

## Local development (without Docker)

### Backend

Requires Go 1.26+, a running Postgres, and a running Redis. Integration tests additionally require a working Docker daemon (they use [testcontainers-go](https://golang.testcontainers.org/) to spin up disposable Postgres/Redis containers) — they skip automatically if Docker isn't available.

```bash
cp .env.example .env
# edit .env: set DATABASE_URL/REDIS_URL if not using the defaults, and
# OPENAI_API_KEY if you want the gateway to actually reach OpenAI

export $(grep -v '^#' .env | xargs)
go run ./cmd/fluxen
```

Manage migrations directly with `fluxenctl`:

```bash
go run ./cmd/fluxenctl migrate up
go run ./cmd/fluxenctl migrate status
go run ./cmd/fluxenctl migrate down
```

Run tests:

```bash
go vet ./...
go test ./...             # requires Docker for integration tests
go test ./... -short      # unit tests only, no Docker required
```

### Frontend

Requires Node.js 22+ and pnpm (`corepack enable` will provide it).

```bash
cd web
pnpm install
pnpm --filter fluxen-dashboard dev   # http://localhost:3000
pnpm --filter fluxen-website dev     # http://localhost:3001
pnpm build                            # builds both apps
```

The dashboard talks to the control API at `NEXT_PUBLIC_API_BASE_URL` (default `http://localhost:8080`) and the gateway at `NEXT_PUBLIC_GATEWAY_BASE_URL` (default `http://localhost:8080/v1`) — both browser-side fetches, so point them at wherever `fluxen` is actually reachable from your browser.

## Configuration

See [`.env.example`](./.env.example) for the full list. The two that matter for a first run:

| Variable | Required | Purpose |
|---|---|---|
| `DATABASE_URL` | yes | Postgres connection string |
| `REDIS_URL` | yes | Redis connection string |
| `OPENAI_API_KEY` | no | The gateway's OpenAI credential — without it, `/v1/chat/completions` returns `503` until set. (Phase 1 uses one deployment-wide key; per-application, UI-managed provider credentials arrive in a later phase.) |
| `FLUXEN_DASHBOARD_ORIGIN` | no | Browser origin the control API allows via CORS (default `http://localhost:3000`) |

## API surface (Phase 1)

**Control API** (session-cookie auth, except setup/login):

```text
GET/POST /api/v1/setup             first-run status / create org + owner
POST     /api/v1/auth/login
POST     /api/v1/auth/logout
GET      /api/v1/auth/session
GET/POST /api/v1/applications
POST     /api/v1/applications/{id}/keys
DELETE   /api/v1/keys/{id}
```

**Gateway** (API-key auth via `Authorization: Bearer fx_live_...`):

```text
POST /v1/chat/completions          OpenAI-compatible, streaming and non-streaming
```

## Repository layout

```text
cmd/fluxen          combined gateway + control binary (the default deployment target)
cmd/fluxenctl        admin CLI (migrations; more subcommands arrive with later phases)
internal/config      env config, validated at boot
internal/auth        password hashing, API keys, session store, key resolver
internal/store       Postgres access: applications, api_keys, users, organizations, requests
internal/gateway     the OpenAI-compatible ingress: auth, pipeline, streaming
internal/ingest      async bounded queue + batch writer from gateway to Postgres
internal/api         the control-plane HTTP API the dashboard talks to
internal/health      dependency health checks (/readyz)
internal/observability  structured logging, Prometheus metrics
pkg/types            provider-neutral request/response/usage types
pkg/providers/openai OpenAI adapter (translation, streaming, errors)
pkg/pricing          embedded pricing catalog + cost calculation
db/migrations/       goose SQL migrations, embedded into the fluxen binary
web/apps/dashboard   the authenticated product (Next.js)
web/apps/website     the public marketing/docs site (Next.js, statically exported)
deploy/              Dockerfiles used by docker-compose.yml
```

See Part C.1 of the implementation specification for the full target module layout, and Part L for what each phase adds.
