# Self-hosting guide

Fluxen has exactly one documented deployment topology: `docker compose up`. There is no Kubernetes operator, Helm chart, or managed-service option in V1.

## Topology

```
docker compose up
```

brings up exactly four services (`docker-compose.yml`):

| Service | Image | Purpose |
|---|---|---|
| `postgres` | `postgres:16-alpine` | System of record — everything except cache/rate-limit/budget counters. |
| `redis` | `redis:7-alpine` | Dashboard sessions, rate-limit/budget counters, exact-caching entries, policy/credential cache invalidation pub/sub. |
| `fluxen` | built from `deploy/Dockerfile.fluxen` | The combined gateway + control API + background job scheduler, one Go binary. |
| `dashboard` | built from `deploy/Dockerfile.dashboard` | The Next.js dashboard, statically built and served. |

The gateway and control API share one process (`cmd/fluxen`) and one port (`8080`) — `/v1/*` is the OpenAI-compatible gateway, `/api/v1/*` is the control API the dashboard talks to, `/healthz`/`/readyz`/`/metrics` are unauthenticated operational endpoints.

## Environment variables

`docker compose up` needs zero configuration: `DATABASE_URL` and `REDIS_URL` are already set by `docker-compose.yml` for container-to-container traffic, and every provider credential (OpenAI, Gemini, Ollama, ...) is added after first login from Settings → Providers, encrypted at rest with a key Fluxen generates and persists itself on first boot — no operator-supplied secret required. See `.env.example` for the full list with inline documentation. Every variable below is optional; it exists for operators who want to override the zero-config defaults, not because any of them are required to boot:

| Variable | Purpose |
|---|---|
| `DATABASE_URL` | Postgres connection string. Already set by `docker-compose.yml`; only needed for local (non-Docker) development. |
| `REDIS_URL` | Redis connection string. Already set by `docker-compose.yml`; only needed for local (non-Docker) development. |
| `OPENAI_API_KEY` | A deployment-wide OpenAI credential, as an alternative to adding one from Settings → Providers. Neither is required to boot — `/v1/chat/completions` returns `503 no_provider_credential` until a credential exists one way or the other. |
| `GEMINI_API_KEY` | Enables Gemini traffic (env-var form — see [Provider setup](./providers.md) for the encrypted, dashboard-managed alternative). |
| `OLLAMA_BASE_URL` | Enables Ollama traffic, e.g. `http://localhost:11434` or a Compose service name. |
| `FLUXEN_ENCRYPTION_KEY` | Base64-encoded 32-byte AES-256 key (`openssl rand -base64 32`) that encrypts Settings → Providers' stored credentials at rest. Leave unset and Fluxen generates one itself on first boot, persisting it to `FLUXEN_DATA_DIR` — set this only to manage the key yourself (e.g. via a secrets manager). |
| `FLUXEN_DATA_DIR` | Where the auto-generated encryption key (and any future local state) is persisted. Defaults to `/data`, backed by its own named volume (`fluxen-data`) in `docker-compose.yml`, separate from the Postgres volume the encrypted data itself lives in. |
| `FLUXEN_DASHBOARD_ORIGIN` | The browser origin the control API allows for credentialed CORS requests. Has a default. |
| `FLUXEN_COOKIE_SECURE` | Set to `true` once TLS terminates in front of Fluxen, so the session cookie is marked `Secure`. Defaults to `false` — required for the plain-HTTP default deployment to work at all. |

Losing the `fluxen-data` volume (e.g. `docker compose down -v`) makes any previously-stored provider credentials undecryptable, exactly like losing any encryption key would — back it up the same way you'd back up the Postgres volume.

## Data retention

Configurable from Settings → Retention (`GET`/`PATCH /api/v1/settings`), enforced daily by the `retention.enforce` background job:

- **Requests retention** (default 90 days): raw request rows older than this are dropped.
- **Body retention** (default 7 days, only relevant if capture is enabled): captured request/response bodies are cleared independently of — and always sooner than — the requests-row retention window.
- **Body capture** (default off): request/response bodies are never captured unless explicitly enabled.

Two things are *not* configurable: dismissed/stale opportunities are always dropped after 180 days, and rollups, efficiency scores, measurements, and policy history are kept indefinitely.

## Background jobs

All run in-process on a simple ticker scheduler (`internal/worker`) — no external broker:

| Job | Cadence | Does |
|---|---|---|
| `rollup.hourly` | every 5 min | Aggregate recent requests into hourly rollups. |
| `rollup.daily` | hourly | Aggregate hourly → daily rollups. |
| `detect.run` | every 6h per app | Run all four detectors. |
| `score.daily` | daily | Compute the Efficiency Score snapshot. |
| `measure.check` | hourly | Fire interim (+7d) / final (+14d) measurement checks whose time has come. |
| `retention.enforce` | daily | Enforce the retention rules above. |

## Security notes

- Provider API keys stored via Settings → Providers are encrypted at rest (AES-256-GCM) and are never returned by any API response — only metadata (provider, status, last health check) is.
- Request/response body capture is off by default; enabling it is an explicit, org-level choice. **Captured bodies are stored unredacted** — exactly as sent/received. There is no redaction of API keys, PII, or other sensitive content embedded in a prompt or response. Only non-streaming responses are captured (streamed response bytes aren't valid JSON for the storage column, so they're never stored regardless of the setting). Treat a deployment with capture enabled as one where request/response content is retained in Postgres, and scope database access accordingly.
- The dashboard and control API communicate over CORS scoped to `FLUXEN_DASHBOARD_ORIGIN`.
- Passwords are hashed with bcrypt; a failed login always runs one bcrypt comparison regardless of whether the email exists, so response timing doesn't leak which accounts are real.
- `/api/v1/auth/login` is rate-limited to 10 attempts per 5 minutes per source IP (Redis-backed, fails open if Redis is briefly unreachable) to blunt brute-force/credential-stuffing.
- Postgres (`5433`) and Redis (`6379`) are published to `127.0.0.1` only by default — reachable from the host machine for local debugging, never exposed to the network a production host sits on. Neither service has a password worth defending on the wire, so don't change this binding to `0.0.0.0` on a host with any public network interface.
- **Fluxen has no built-in TLS.** `docker compose up` serves plain HTTP on `8080`/`3000`. For any deployment reachable over an untrusted network, put a TLS-terminating reverse proxy (Caddy, nginx, Traefik, a cloud load balancer) in front of it, and set `FLUXEN_COOKIE_SECURE=true` once you do — otherwise the session cookie travels in the clear and can be intercepted.
- All control-API request bodies are capped at 1 MiB (`internal/api`'s `bodySizeLimitMiddleware`); the gateway independently caps chat-completion request bodies (`gateway.MaxRequestBodyBytes`).

## Load testing

`tools/mockupstream` is a throwaway OpenAI-compatible stub (instant, fixed responses) for load-testing the gateway's own overhead in isolation from a real provider's latency — it's never built into the production image (`deploy/Dockerfile.fluxen` only builds `cmd/fluxen`/`cmd/fluxenctl`). Point a provider credential's base URL at it, then load-test `/v1/chat/completions` with any HTTP load tool (e.g. [`hey`](https://github.com/rakyll/hey)).

This is how a real bottleneck was found and fixed during hardening: `internal/auth.Resolver` (the gateway's API-key cache) re-ran a full bcrypt comparison on every request even on a cache hit, capping realistic throughput at ~150 req/s regardless of hardware. It now caches a SHA-256 fingerprint of the already-bcrypt-verified raw key and only re-runs bcrypt when that fingerprint doesn't match (a rotation, or a wrong guess) — see `Resolver`'s doc comment in `internal/auth/keyresolver.go`. Measured before/after on the same machine, same mock upstream, 2,000 requests at 50 concurrent: **149 req/s → 6,049 req/s**, P99 latency 537ms → 63ms.

## Upgrading

`docker compose up --build` after pulling. Migrations run automatically and idempotently at boot — there is no separate migration step.
