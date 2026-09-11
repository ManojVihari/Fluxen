# Fluxen

**AI Traffic, Optimized.**

Fluxen is a self-hosted AI traffic gateway and optimization platform. See:

- [`Fluxen V1 — Product Requirements Document.md`](./Fluxen%20V1%20—%20Product%20Requirements%20Document.md) — what Fluxen is and why.
- [`Fluxen V1 — Implementation Plan.md`](./Fluxen%20V1%20—%20Implementation%20Plan.md) — the developer-ready implementation specification and phased build plan. This is the primary engineering reference; read it before changing code.

## Current status

**Phase 0 — Foundation** is implemented: repository structure, local dev loop, Postgres + Redis connectivity with migrations, structured logging, health/readiness/metrics endpoints, and a minimal Next.js scaffold for the dashboard and public website. There is no gateway, no application concept, and no product behavior yet — that begins in Phase 1.

## Quickstart (Docker Compose)

Requires Docker and Docker Compose.

```bash
docker compose up --build
```

This brings up four services:

| Service | Purpose | Port |
|---|---|---|
| `postgres` | Primary datastore | 5432 |
| `redis` | Cache / counters | 6379 |
| `fluxen` | Combined gateway + control binary | 8080 |
| `dashboard` | Next.js dashboard | 3000 |

Database migrations are applied automatically by the `fluxen` binary at startup.

Verify it's up:

```bash
curl http://localhost:8080/healthz   # liveness — process is serving
curl http://localhost:8080/readyz    # readiness — postgres + redis reachable
curl http://localhost:8080/metrics   # Prometheus metrics
```

Open the dashboard placeholder at [http://localhost:3000](http://localhost:3000).

## Local development (without Docker)

### Backend

Requires Go 1.26+, a running Postgres, and a running Redis.

```bash
cp .env.example .env
# edit .env to point at your local Postgres/Redis if not using the defaults

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
go test ./...
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

## Repository layout

```text
cmd/fluxen        combined gateway + control binary (the default deployment target)
cmd/fluxenctl     admin CLI (migrations; more subcommands arrive with later phases)
internal/         service-private Go packages (config, auth, health, observability, store, ...)
pkg/              shared, importable Go packages (added starting in Phase 1)
db/migrations/    goose SQL migrations, embedded into the fluxen binary
web/apps/dashboard  the authenticated product (Next.js)
web/apps/website    the public marketing/docs site (Next.js, statically exported)
deploy/           Dockerfiles used by docker-compose.yml
```

See Part C.1 of the implementation specification for the full target module layout.
