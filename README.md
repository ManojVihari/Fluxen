# Fluxen

**AI Traffic, Optimized.**

Fluxen is a self-hosted AI traffic gateway and optimization platform. See:

- [`Fluxen V1 — Product Requirements Document.md`](./Fluxen%20V1%20—%20Product%20Requirements%20Document.md) — what Fluxen is and why.
- [`Fluxen V1 — Implementation Plan.md`](./Fluxen%20V1%20—%20Implementation%20Plan.md) — the developer-ready implementation specification and phased build plan. This is the primary engineering reference; read it before changing code.

## Current status

**Phase 0 — Foundation**, **Phase 1 — First AI Request**, and **Phase 2 — First Understanding** are implemented.

- Repository structure, local dev loop, Postgres + Redis connectivity with migrations, structured logging, health/readiness/metrics endpoints.
- A control API (`/api/v1/...`) for first-run setup, login/logout, applications, and API keys.
- An OpenAI-compatible gateway (`/v1/chat/completions`) that authenticates by API key, proxies to real OpenAI (streaming and non-streaming), and persists every request — attributed, priced, and token-counted — to Postgres.
- An in-process background scheduler that rolls raw requests up into hourly/daily aggregates, and a control API surface (`summary`/`timeseries`/`models`) that reads them.
- A dashboard: setup wizard, login, applications list (with 30-day spend/requests), an Application Detail view (Usage & Cost and Models tabs), and a connect screen that issues an API key once with copy-paste Python/Node/curl snippets.
- `fluxenctl seed --demo` — seeds a demo organization, a `document-ai` application, and ~30 days of realistic synthetic traffic (with a deliberately over-used premium model) so the product can be explored without waiting for real traffic.

Not yet implemented (later phases, per the spec): any optimization opportunity/detector, efficiency score, Overview page, Requests investigation screen, or Policies (Phases 3–6), and Gemini/Ollama/caching/rate limits/budgets/model routing (Phases 5, 7).

### What's new in Phase 2

Phase 2's goal is: Fluxen can understand an application's AI traffic well enough to show it back to the user. Concretely, this phase added:

| Area | What it does |
|---|---|
| `internal/worker` | An in-process background job scheduler — runs each registered job immediately at boot, then on its own interval, for the life of the process. |
| `internal/rollup` | Turns raw `requests` rows into hourly and daily aggregates. Idempotent: recomputing a time window deletes and re-inserts it, so a job re-run (or ingest lag catching up) never double-counts. Runs as two scheduled jobs: `rollup.hourly` (every 5 min) and `rollup.daily` (every hour). |
| `db/migrations/00004` | Three new tables: `request_rollup_hourly`, `request_rollup_daily`, `application_daily` — nothing in the API reads `requests` directly for these numbers. |
| `GET .../summary`, `.../timeseries`, `.../models` | Three new control-API endpoints, all accepting `?range=24h\|7d\|30d\|90d` (default `30d`), reading only the rollup tables. |
| Application Detail (dashboard) | A new screen at `/applications/{id}` with a shared header + tab nav. **Usage & Cost** tab: spend, requests, tokens, error rate, and average latency stat tiles, plus a daily-spend bar chart. **Models** tab: a table of every (provider, model) pair the application used, sorted by cost, with per-model requests/tokens/errors/latency/spend/share. Efficiency, Opportunities, Policies, and Requests tabs are visible but disabled — placeholders for later phases. |
| Applications list (dashboard) | Now shows 30-day spend and request count per application, not just name/status. |
| `tools/trafficgen` + `fluxenctl seed --demo` | Generates ~30 days of realistic synthetic traffic for a demo `document-ai` application, split across two real (catalog-priced) models with a **deliberately over-used premium model** — most of that traffic has a token profile a cheaper model could plausibly have handled, which is exactly the inefficiency Phase 3's Model Cost detector will be built to find. Idempotent — safe to run more than once; reuses an existing organization/application if one already exists. |

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

### See it with demo data

The fastest way to see Application Detail with real-looking data, without connecting anything yourself:

```bash
docker compose exec fluxen fluxenctl seed --demo
```

This creates an organization (owner: `owner@example.com` / `supersecret123`, only if no organization exists yet), an application named "Document AI", and ~30 days of synthetic multi-model traffic — then sign in and open it from **Applications**. Safe to run more than once; it no-ops if the application already has traffic.

### Connect a real application

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

5. Open the application in the dashboard — usage, cost, and model mix appear within a few minutes (the rollup job that powers Application Detail runs every 5 minutes; a fresh request is visible sooner via the API, `GET /api/v1/applications/{id}/summary`, but the dashboard reads the rolled-up view).

For the gateway to actually reach OpenAI, set `OPENAI_API_KEY` before starting the stack — without it, the gateway still runs, but every chat request returns `503 no_provider_credential`.

```bash
export OPENAI_API_KEY=sk-...
docker compose up --build
```

## Verifying this build

### Automated

```bash
go vet ./...
go test ./... -count=1     # full suite, requires Docker (testcontainers)
go test ./... -short       # unit tests only, no Docker required

cd web && pnpm install && pnpm build   # both apps must build with no type errors
```

What each package's tests actually prove, if you want to spot-check rather than trust a green run:

| Package | Proves |
|---|---|
| `pkg/types` | Unknown request fields round-trip a Parse→JSON cycle byte-for-byte; workload-feature extraction (tools/images/tool-calls/JSON-mode) is correct. |
| `pkg/pricing` | Cost calculation is exact integer micro-USD math; an uncataloged model never fabricates a price. |
| `pkg/providers/openai` | Request/response translation is lossless; streaming chunks are forwarded verbatim while usage is still extracted; upstream errors relay through unchanged. |
| `internal/auth` | API keys and sessions issue, verify, and revoke correctly; the resolver's cache serves repeated lookups without hitting Postgres, and `Invalidate` bypasses it immediately after a revoke. |
| `internal/gateway` | The full pipeline works end to end (streaming and non-streaming) against a fake provider; a client disconnect mid-stream still produces a `client_abort` usage record; a revoked key gets 401 at the gateway itself. |
| `internal/rollup` | Hourly/daily aggregation is arithmetically correct against hand-computed fixtures, and **idempotent** — running the same window 2–3 times never changes the result. |
| `internal/store` | Every query is scoped correctly (an org can never read another org's data); rollup read-queries (`Summary`/`Timeseries`/`ModelBreakdown`) return correct sums and sort order. |
| `internal/api` | The complete setup→login→create-app→issue-key→revoke→logout journey; the range-scoped rollup endpoints return exactly the seeded numbers (this is also where a same-day date-boundary bug was caught before it ever reached a demo). |
| `internal/worker` | Jobs run immediately at boot, then on their own interval; one job's failure never stops another job or the scheduler. |
| `tools/trafficgen` | Generated traffic only uses cataloged (priced) models; the injected model-cost inefficiency is actually present in the output; generation is deterministic for a fixed seed; `Seed()` is idempotent end-to-end against real Postgres. |

### Manual, against the running stack

With `docker compose up --build` healthy (see Quickstart), a few things worth checking by hand beyond what CI covers:

1. **Seed and inspect the data pipeline directly:**
   ```bash
   docker compose exec fluxen fluxenctl seed --demo
   docker compose exec fluxen fluxenctl seed --demo   # run again — should print "already has traffic"
   docker compose exec postgres psql -U fluxen -d fluxen -c \
     "SELECT count(*) FROM requests; SELECT count(*) FROM application_daily;"
   ```
2. **Confirm the rollup endpoints reflect it** (grab the app id from `GET /api/v1/applications` after logging in — see API surface below for the full request shapes):
   ```bash
   curl -s -b cookies.txt "http://localhost:8080/api/v1/applications/$APP_ID/summary?range=30d"
   curl -s -b cookies.txt "http://localhost:8080/api/v1/applications/$APP_ID/models?range=30d"
   curl -s -b cookies.txt "http://localhost:8080/api/v1/applications/$APP_ID/timeseries?range=30d"
   ```
   The models breakdown should show the premium model carrying a disproportionate share of cost relative to its request count — that's the deliberately injected inefficiency. The timeseries should include **today** as its last point (a real bug — a same-day exclusion off-by-one in the range boundary — was caught here during development; if today's point is ever missing again, that regression is back).
3. **Open the dashboard** at `http://localhost:3000`, sign in (`owner@example.com` / `supersecret123` if you used `--demo` on a fresh install), and open the seeded application: the Usage & Cost tab's stat tiles and bar chart, and the Models tab's table, should match what you saw via curl in step 2.
4. **Send one real request** (see "Connect a real application" above) and confirm it shows up in `GET .../summary` within a few minutes (rollup.hourly ticks every 5 minutes; it also runs once immediately at process boot).
5. **Restart the stack** (`docker compose restart fluxen` or a full `down`/`up`) and confirm migrations report "no migrations to run" and the seeded data is still there (volumes persist unless you pass `-v` to `down`).

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

Manage migrations and demo data with `fluxenctl`:

```bash
go run ./cmd/fluxenctl migrate up
go run ./cmd/fluxenctl migrate status
go run ./cmd/fluxenctl migrate down
go run ./cmd/fluxenctl seed --demo
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

## API surface

**Control API** (session-cookie auth, except setup/login):

```text
GET/POST /api/v1/setup                            first-run status / create org + owner
POST     /api/v1/auth/login
POST     /api/v1/auth/logout
GET      /api/v1/auth/session
GET/POST /api/v1/applications
GET      /api/v1/applications/{id}/summary?range=  cost/usage/errors/latency, one range window
GET      /api/v1/applications/{id}/timeseries?range=   the same, one row per day
GET      /api/v1/applications/{id}/models?range=   breakdown by (provider, model), cost descending
POST     /api/v1/applications/{id}/keys
DELETE   /api/v1/keys/{id}
```

`range` accepts `24h`, `7d`, `30d` (default), `90d`.

**Gateway** (API-key auth via `Authorization: Bearer fx_live_...`):

```text
POST /v1/chat/completions          OpenAI-compatible, streaming and non-streaming
```

## Repository layout

```text
cmd/fluxen           combined gateway + control binary (the default deployment target)
cmd/fluxenctl        admin CLI (migrations, demo seeding)
internal/config      env config, validated at boot
internal/auth        password hashing, API keys, session store, key resolver
internal/store       Postgres access: applications, api_keys, users, organizations, requests, rollups
internal/gateway     the OpenAI-compatible ingress: auth, pipeline, streaming
internal/ingest      async bounded queue + batch writer from gateway to Postgres
internal/rollup      idempotent hourly/daily aggregation of requests into the rollup tables
internal/worker      the in-process background job scheduler
internal/api         the control-plane HTTP API the dashboard talks to
internal/health      dependency health checks (/readyz)
internal/observability  structured logging, Prometheus metrics
pkg/types            provider-neutral request/response/usage types
pkg/providers/openai OpenAI adapter (translation, streaming, errors)
pkg/pricing          embedded pricing catalog + cost calculation
tools/trafficgen     synthetic multi-day, multi-model traffic generator (used by `fluxenctl seed`)
db/migrations/       goose SQL migrations, embedded into the fluxen binary
web/apps/dashboard   the authenticated product (Next.js)
web/apps/website     the public marketing/docs site (Next.js, statically exported)
deploy/              Dockerfiles used by docker-compose.yml
```

See Part C.1 of the implementation specification for the full target module layout, and Part L for what each phase adds.
