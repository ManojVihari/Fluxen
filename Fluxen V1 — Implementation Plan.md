# Fluxen V1 — Implementation Specification

**Companion to:** `Fluxen V1 — Product Requirements Document.md`
**Status:** Developer-ready implementation specification — primary execution guide
**Stack:** Go (gateway + control/API + workers) · Next.js/React/TypeScript/Tailwind (dashboard + website) · PostgreSQL · Redis
**Deployment target:** `docker compose up` — single host, no orchestration platform

> **The gateway is the mechanism. Optimization is the product.**
> Core loop: `Understand → Identify → Simulate → Control → Measure`

This document is the single source of truth for building Fluxen V1. It is written so that each phase can be executed independently, in order, without re-deriving product decisions. Where the PRD and this document differ in wording, this document governs implementation; where this document is silent on a product question, the PRD governs intent.

---

# Part A — Product Boundary

## A.1 V1 providers (exactly these three)

- OpenAI
- Google Gemini
- Ollama

No other provider is implemented in V1: not Anthropic, not AWS Bedrock, not Azure OpenAI, not Vertex AI, not Mistral, not Cohere. The `Provider` interface (§C.4) is written to make adding one *possible later*, but no second implementation of a fourth provider is built now, regardless of how small the diff looks.

## A.2 Explicitly out of scope for V1

Do not implement any of the following. If a task seems to require one of these, stop and flag it rather than building a partial version.

| Category | Excluded |
|---|---|
| Caching | Semantic caching, embeddings, vector database, similarity search |
| Quality | LLM-as-judge, quality benchmarking, golden datasets, quality-equivalence claims |
| Prompts | Prompt management, prompt versioning, prompt playground, prompt marketplace |
| Agents | Agent tracing, tool-call tracing, MCP governance, multi-step execution graphs |
| Autonomy | Autonomous optimization, automatic production changes without human confirmation |
| Enterprise | SSO, SCIM, fine-grained RBAC, audit/compliance platform |
| Infra | Kubernetes operator, Helm chart, service mesh, Kafka, distributed event bus, microservices beyond gateway/control/worker |
| Observability | Full tracing platform, generic APM, custom dashboard/query builder |
| Cost | Electricity/GPU infrastructure cost modeling for Ollama |

These are roadmap items (PRD §29–34), not V1 tasks. A feature being easy to add is not a reason to add it.

## A.3 V1 capability list (frozen)

Fluxen V1 delivers exactly these capabilities. Nothing more, nothing less:

1. **Gateway** — OpenAI-compatible ingress, application-scoped API keys, routing to OpenAI/Gemini/Ollama, streaming and non-streaming, usage extraction, cost calculation, exact caching, rate limiting, budgets, model restrictions, percentage routing.
2. **Application model** — applications as the unit of optimization, with an AI Efficiency Profile.
3. **Cost intelligence** — versioned pricing catalog, integer micro-USD math, honest unknown-cost handling.
4. **Four detectors** — model cost, repeated request, token efficiency, traffic anomaly.
5. **Simulation** — model mix, exact caching, budget impact, replayed against real historical facts.
6. **Apply + Measure** — human-confirmed policy changes, frozen baselines, before/after verdicts, revert.
7. **Dashboard** — Overview, Applications, Application Detail, Requests, Optimizations, Policies, Settings.
8. **Public website** — Home, Product, How it Works, Why Fluxen, Providers, Pricing, Docs/Getting Started.

---

# Part B — Architecture

## B.1 Processes

Three logical services, deployable as three containers or one combined binary:

```text
fluxen-gateway    Hot path. Stateless. Horizontally scalable. Never blocks on analytics.
fluxen-control    HTTP API for the dashboard + background job runner (detectors, rollups,
                  simulation, measurement, scoring). One process, in-process job scheduler.
fluxen-dashboard  Next.js app (dashboard + website as two Next.js apps, or one app with
                  route groups — see B.3).
```

This is not a microservices system. `fluxen-control` runs its scheduler in-process (no separate worker fleet, no message broker). `fluxen-gateway` and `fluxen-control` share Go modules (`pkg/`) but are separate binaries because the gateway's uptime and latency requirements are stricter than the control plane's — a slow detector run must never be able to slow down a proxied request. That is the only reason for the split; it is not the first step toward a service mesh.

For very small deployments, `cmd/fluxen` combines gateway + control in one process, one container, one port pair. This is the default in `docker-compose.yml`. Splitting into two containers is an opt-in scaling step, not the V1 default.

## B.2 Hot-path contract (non-negotiable)

A request proxied through the gateway must never fail, stall, or slow down because of:

- a database being slow or unavailable,
- a detector or rollup job running,
- the control-plane process being down.

Usage records are written to an in-memory bounded queue and flushed asynchronously. If the queue is full, records are dropped and a counter (`fluxen_usage_dropped_total`) increments — **serving traffic always wins over recording it**. If Postgres is unreachable, the gateway continues serving traffic using its last-known policy snapshot (cached in-process, revalidated opportunistically) and using Redis-only counters for budget/rate-limit; if Redis is also unreachable, rate limiting and budgets **fail open** (log + metric, do not block traffic) — an infrastructure outage must not become a customer-facing AI outage. This is rule 20 in Part F.

## B.3 Frontend structure

Two Next.js apps in one pnpm workspace:

- `apps/dashboard` — the authenticated product (Overview, Applications, Requests, Optimizations, Policies, Settings).
- `apps/website` — the public marketing/docs site, statically exported, deployed independently.

They are separate apps (not route groups in one app) because they have different auth models, different deploy cadences, and the website must be able to ship without touching the authenticated product.

## B.4 Deployment

`docker-compose.yml` brings up exactly: `postgres`, `redis`, `fluxen` (combined gateway+control binary), `dashboard`. That's four containers. No message broker, no separate worker container, no reverse-proxy container by default (documented as an optional addition). This is the only deployment topology V1 supports and documents.

---

# Part C — Backend Design (Go)

## C.1 Module layout

```text
fluxen/
├── cmd/
│   ├── fluxen/          # combined gateway+control+scheduler binary (default)
│   ├── fluxen-gateway/  # gateway-only binary (opt-in split deployment)
│   ├── fluxen-control/  # control-only binary (opt-in split deployment)
│   └── fluxenctl/       # admin CLI: migrate, create-org, issue-key, seed demo traffic
├── internal/
│   ├── config/          # env-based config, validated at boot
│   ├── gateway/          # HTTP handlers, pipeline stages, streaming
│   ├── auth/             # session auth (dashboard), API key auth (gateway)
│   ├── guard/             # rate limit, budget, model restriction enforcement
│   ├── cache/             # exact cache: key, eligibility, store, replay
│   ├── route/             # percentage routing decision
│   ├── ingest/             # usage record queue, batch writer
│   ├── rollup/             # hourly/daily aggregation jobs
│   ├── detect/             # the four detectors + registry
│   ├── score/              # efficiency score computation
│   ├── sim/                # simulation engine
│   ├── measure/            # baseline freeze, comparison, verdict
│   ├── policy/             # policy document, apply/revert, history
│   ├── api/                # control-plane HTTP API (dashboard-facing)
│   ├── store/              # Postgres access (sqlc-generated + hand-written)
│   ├── worker/             # in-process scheduler (cron-like)
│   └── observability/      # logging, metrics, health
├── pkg/
│   ├── types/             # CanonicalRequest/Response, UsageRecord, IDs, Money
│   ├── providers/         # Provider interface + openai/gemini/ollama adapters
│   ├── pricing/            # catalog + cost calculation (pure)
│   └── policy/             # PolicyDocument schema + pure evaluator
├── db/
│   ├── migrations/         # goose SQL migrations
│   └── queries/             # sqlc source
├── api/
│   └── openapi.yaml         # single API contract, source for Go stubs + TS client
└── web/
    ├── apps/dashboard
    └── apps/website
```

## C.2 The critical purity invariant

`pkg/pricing` and `pkg/policy` contain **no I/O**: no database calls, no HTTP calls, no wall-clock reads except through an injected clock, no randomness except through an injected source. They are pure functions of their inputs.

This is enforced because both the gateway (production) and `internal/sim` (simulation) call the exact same functions:

```go
decision := policy.Evaluate(policyDoc, requestFacts, rng)
cost      := pricing.Calculate(usage, model, catalogVersion)
```

If simulation ever reimplements this logic instead of importing it, simulated and actual numbers can silently diverge, and Part on Measurement (§G) becomes untrustworthy. **Rule 9 in Part F makes this a standing rule, not a one-time design choice.**

## C.3 Domain types (`pkg/types`)

```go
// Money is always integer micro-USD. 1 USD = 1_000_000. Never float64 for cost.
type Money int64

// A cost that could not be determined — distinguishes "$0" from "unknown."
type CostStatus string
const (
    CostKnown     CostStatus = "known"     // priced from catalog
    CostUnknown   CostStatus = "unknown"   // no catalog entry — never fabricated
    CostLocal     CostStatus = "local"     // Ollama: no provider bill exists
)

type CanonicalRequest struct {
    Model       string
    Messages    []Message
    Tools       []Tool
    ToolChoice  json.RawMessage
    ResponseFormat json.RawMessage
    Temperature *float64
    TopP        *float64
    MaxTokens   *int
    Stop        []string
    Seed        *int
    N           *int
    Stream      bool
    Extra       map[string]json.RawMessage // unrecognized fields pass through untouched
}

type UsageRecord struct {
    RequestID       string
    AppID           AppID
    StartedAt       time.Time
    DurationMS      int
    TTFTMS          *int
    Endpoint        string   // "chat.completions" | "embeddings" | "generate"
    Protocol        string   // "openai" | "gemini" | "ollama"
    Streamed        bool
    RequestedModel  string
    Provider        string
    Model           string
    RouteReason     string   // "direct" | "split" | "restriction" | "fallback"
    RouteVariant    string   // "" | "A" | "B"
    PolicyVersion   int
    InputTokens     int
    OutputTokens    int
    CachedInputTokens int
    TotalTokens     int
    UsageSource     string   // "provider" | "estimated"
    Cost            Money
    CostInput       Money
    CostOutput      Money
    CostStatus      CostStatus
    PricingVersion  string
    CacheStatus     string   // "hit" | "miss" | "bypass" | "disabled"
    CacheKey        []byte
    CacheSavedCost  Money
    Status          string   // "ok" | "provider_error" | "blocked" | "timeout" | "client_abort"
    HTTPStatus      int
    ErrorCode       string
    // workload-profile features, used by detectors — no bodies required
    HasTools, HasToolCalls, HasImages, JSONMode bool
    Temperature     *float64
    MaxTokensReq    *int
    MessageCount    int
    SystemPromptHash []byte
}
```

## C.4 Provider abstraction

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req *types.CanonicalRequest, cred Credential) (*types.CanonicalResponse, error)
    ChatStream(ctx context.Context, req *types.CanonicalRequest, cred Credential) (StreamReader, error)
    Models(ctx context.Context, cred Credential) ([]ModelInfo, error)
    Health(ctx context.Context, cred Credential) error
}
```

Three implementations: `pkg/providers/openai`, `pkg/providers/gemini`, `pkg/providers/ollama`. See Part D for the compatibility contract each must satisfy.

## C.5 Gateway pipeline

Ordered, explicit stages (`internal/gateway/pipeline.go`):

```text
1. Authenticate     fx_ key → AppID              (401 on failure, no DB hit on cache hit)
2. Parse            protocol-specific → CanonicalRequest
3. Load policy      in-process snapshot, 5s TTL, Redis pub/sub invalidation
4. Model restrict    403 model_not_allowed if not on allowlist
5. Rate limit         429 with Retry-After (Redis token bucket; fail-open if Redis down)
6. Budget check        402 budget_exceeded if hard limit hit (fail-open if Redis down)
7. Cache lookup          hit → skip to step 11
8. Route                choose provider+model via percentage split
9. Invoke                 call provider, unary or streamed
10. Account                extract usage, calculate cost
11. Emit                    push UsageRecord to async queue
12. Respond                  return response to caller
```

Every stage that can reject a request produces a canonical `X-Fluxen-Error-Code` header and a response body shaped like the client's own protocol (OpenAI-shaped error for `/v1/*` calls, etc.).

## C.6 Streaming

- Tee the upstream stream to the client immediately; never buffer the full body before forwarding.
- Flush after every chunk (`http.Flusher`).
- Extract usage from the final chunk per provider (OpenAI: `stream_options.include_usage`; Gemini: `usageMetadata` on final chunk; Ollama: `prompt_eval_count`/`eval_count` on final object).
- On client disconnect: cancel upstream, still emit a `UsageRecord` with `status="client_abort"` and whatever usage was observed — an aborted stream still cost money.
- On provider error mid-stream: emit an error event in the client's dialect, terminate, record partial usage.

## C.7 Background jobs (in-process scheduler)

`internal/worker` runs a simple in-process ticker-based scheduler (no external broker — this is a monolith-friendly cron, not a distributed queue):

| Job | Cadence | Does |
|---|---|---|
| `rollup.hourly` | every 5 min | aggregate recent requests into hourly rollups |
| `rollup.daily` | hourly | aggregate hourly → daily, update `application_daily` |
| `detect.run` | every 6h per app | run all four detectors |
| `score.daily` | daily | compute efficiency score snapshot |
| `measure.check` | every 15 min | fire interim (+7d) / final (+14d) measurements whose time has come |
| `retention.enforce` | daily | drop expired partitions/bodies |
| `provider.health` | every 5 min | ping configured provider credentials |

If this outgrows a single ticker loop, the escalation path is documented in Part J — it is explicitly not built in V1.

---

# Part D — Provider Compatibility Contract

Fluxen does not pretend the three providers are identical. This table is the ground truth for what is guaranteed vs. unsupported.

| Capability | OpenAI | Gemini | Ollama |
|---|---|---|---|
| Chat (non-streaming) | ✅ full | ✅ via translation | ✅ full |
| Chat (streaming) | ✅ full | ✅ via translation | ✅ full |
| Usage extraction | ✅ provider-reported | ✅ provider-reported (`usageMetadata`) | ✅ provider-reported (`eval_count`) |
| Model discovery | ✅ static catalog + `/v1/models` passthrough | ✅ static catalog | ✅ live via `/api/tags` |
| Tools/function calling | ✅ | ✅ translated (`functionDeclarations`) | ⚠️ model-dependent, passed through untranslated |
| Vision | ✅ | ✅ | ⚠️ model-dependent |
| JSON/schema response format | ✅ `response_format` | ✅ translated (`responseSchema`) | ⚠️ best-effort, not guaranteed enforced |
| Percentage routing | ✅ | ✅ | ✅ |
| Exact caching | ✅ | ✅ | ✅ |
| Budget/rate limit | ✅ | ✅ | ✅ (Ollama cost = unknown, see D.1; rate/limit still enforced on request count) |
| Cost calculation | ✅ catalog-priced | ✅ catalog-priced | ❌ no provider bill — see D.1 |
| Fallback on connection failure | ✅ opt-in, one retry to primary model | ✅ | ✅ |
| Intelligent/automatic routing by quality | ❌ not in V1, any provider | | |

## D.1 Ollama cost handling (frozen decision)

Ollama has no provider invoice. V1 does **not** build a GPU/electricity cost model (explicitly excluded, Part A.2). Every Ollama `UsageRecord` gets `CostStatus = CostLocal` and `Cost = 0`. The UI never shows this as "$0.00" bare — it renders as **"Local · not billed"**, visually distinct from priced cost, and Ollama spend is excluded from every cost/savings sum unless a view explicitly opts into showing "local traffic" as a separate labeled row. This prevents a diagram like "compare cloud and local cost" from silently implying local is free money.

## D.2 Unknown pricing (frozen decision)

If a model is used that has no catalog entry (new model, typo, provider added a variant Fluxen doesn't know about), `CostStatus = CostUnknown`, `Cost = 0`, and the UI shows **"Cost unknown"**, never a fabricated `$0.00`. Requests with unknown cost are **excluded** from all detector cost-savings math and from the efficiency score's cost components — an unknown value must never silently become a zero that makes a low-usage app look artificially efficient or produces phantom "100% savings."

---

# Part E — Data Model

Full DDL lives in `db/migrations/`. This section defines purpose, key fields, relationships, lifecycle, and retention per entity. No table exists here "because it might be useful later" — each one is read by a named feature.

| Entity | Purpose | Lifecycle | Retention |
|---|---|---|---|
| `organizations` | Tenant boundary. V1 is single-org per deployment but the column exists for correctness. | created once at setup | forever |
| `users` | Dashboard accounts. Roles: `owner`, `member`. | created at setup / invited | forever |
| `applications` | **The fundamental unit of Fluxen.** | `active → paused → archived` | forever (soft state) |
| `api_keys` | Attribution mechanism: a key identifies exactly one application. | `active → revoked` | forever, revoked keys kept for audit |
| `provider_credentials` | Org-scoped, encrypted provider API keys / Ollama base URLs. | `active → revoked` | forever |
| `requests` | Raw fact table. One row per proxied request. Monthly partitioned. | append-only | 90 days default, configurable |
| `request_rollup_hourly` / `_daily` | Aggregates for charts; nothing in the UI queries `requests` directly beyond the Requests investigation screen. | rebuilt by jobs | forever |
| `application_daily` | One row per app per day — the input to Applications table and the score. | rebuilt daily | forever |
| `policies` | Current policy document per application (one row per app). | mutated in place, versioned | current only |
| `policy_history` | Every policy mutation, with diff and reason. | append-only | forever |
| `opportunities` | Detector output. **The product's primary noun.** | see Part G lifecycle | dismissed/stale kept 180d then dropped |
| `simulations` | A scenario run against historical facts. | append-only | 90 days |
| `measurements` | Before/after outcome of an applied opportunity. | `collecting → interim → final` or `reverted` | forever |
| `efficiency_scores` | Daily score snapshot per app. | append-only, one per app per day | 180 days |

## E.1 Key fields (abbreviated — full DDL in migrations)

```sql
applications(id, org_id, slug UNIQUE(org_id,slug), name, status, first_seen_at, last_seen_at)

api_keys(id, app_id, name, prefix UNIQUE, key_hash, revoked_at)

requests(  -- PARTITION BY RANGE (started_at), PK (started_at, id)
  id, org_id, app_id, api_key_id, started_at, duration_ms, ttft_ms,
  endpoint, protocol, streamed,
  requested_model, provider, model, route_reason, route_variant, policy_version,
  input_tokens, output_tokens, cached_input_tokens, total_tokens, usage_source,
  cost_micro, cost_input_micro, cost_output_micro, cost_status, pricing_version,
  cache_status, cache_key, cache_saved_micro,
  status, http_status, error_code,
  has_tools, has_tool_calls, has_images, json_mode, temperature, max_tokens,
  message_count, system_prompt_hash,
  request_body jsonb NULL, response_body jsonb NULL  -- off by default
)
-- indexes: (app_id, started_at desc), (app_id, model, started_at desc),
--          (app_id, cache_key, started_at desc) WHERE cache_key IS NOT NULL

opportunities(
  id, org_id, app_id, kind, fingerprint, status, severity, title, summary,
  window_start, window_end, sample_requests,
  current_cost_micro, projected_cost_micro, savings_micro, savings_pct,
  confidence, confidence_score, evidence jsonb, recommendation jsonb,
  detector_version, detected_at, reviewed_at, reviewed_by, dismissed_at, dismiss_reason,
  last_seen_at
)
-- UNIQUE(app_id, fingerprint) WHERE status IN ('open','reviewed','simulated')

simulations(
  id, org_id, app_id, opportunity_id NULL, scenario jsonb,
  window_start, window_end, replayed_requests, affected_requests,
  actual_cost_micro, simulated_cost_micro, delta_micro, delta_pct,
  projected_monthly_savings_micro, breakdown jsonb, assumptions jsonb,
  engine_version, created_by, created_at
)

policies(app_id PK, version, document jsonb, updated_at, updated_by)
policy_history(id, app_id, version, document jsonb, diff jsonb, change_source,
                opportunity_id NULL, simulation_id NULL, note, changed_by, changed_at)

measurements(
  id, app_id, opportunity_id, simulation_id, policy_version, applied_at,
  baseline_start, baseline_end, baseline_requests, baseline_cost_micro,
  baseline_cost_per_1k_micro,
  observed_start, observed_end, observed_requests, observed_cost_micro,
  observed_cost_per_1k_micro,
  expected_savings_micro, actual_savings_micro, expected_pct, actual_pct,
  verdict, verdict_reason, status, finalized_at
)

efficiency_scores(app_id, day, score, model_eff, token_eff, cache_eff,
                   traffic_stability, cost_eff, reasons jsonb, version, PK(app_id,day))
```

Money is `bigint` micro-USD everywhere, no exceptions. `cost_status` (`known|unknown|local`) is a first-class column on `requests`, not inferred from `cost_micro = 0`.

## E.2 Retention (V1 defaults, configurable in Settings)

- `requests`: 90 days (partition drop)
- `request_body`/`response_body`: 7 days (separate, shorter — off by default entirely)
- rollups, scores, opportunities (open/applied), measurements, policy history: kept indefinitely
- dismissed/stale opportunities: 180 days

---

# Part F — Development Rules for Claude

These rules apply for the entire duration of building Fluxen V1. They are permanent, not phase-specific.

1. Read the relevant section of this specification (and the PRD where referenced) before changing code.
2. Do not invent product behavior when the specification is ambiguous. Ambiguity is a stop signal, not a creative-freedom signal.
3. If ambiguity affects architecture or product behavior (not implementation detail), stop and ask rather than guessing.
4. Do not expand V1 scope. Part A.2 is a hard boundary, not a suggestion.
5. Do not introduce a dependency without a concrete V1 requirement it solves. No library "because it might help later."
6. Prefer the simplest implementation that satisfies the current phase's acceptance criteria over an abstraction built for a hypothetical future requirement.
7. Keep provider-specific code isolated inside `pkg/providers/{openai,gemini,ollama}`. No provider-specific branching in `internal/gateway` beyond selecting which adapter to call.
8. Keep business logic (pricing, policy evaluation, detectors, simulation, scoring) testable without HTTP or a browser — pure functions/structs with injected dependencies, not handlers doing the work inline.
9. Never duplicate pricing or policy logic between production and simulation. Both call `pkg/pricing` and `pkg/policy`. If simulation needs behavior production doesn't have, add it to the shared package, not a simulation-only copy.
10. Do not silently change database semantics (column meaning, unit, nullability) without a migration and a note in this document's Part E.
11. Do not break an existing API contract (`api/openapi.yaml`) without an explicit versioning/migration decision, stated in the PR description.
12. Add tests that assert real behavior (a detector fires on injected inefficiency and stays silent on clean traffic; a policy change blocks a request) — not tests that only exist to raise a coverage number.
13. Run the relevant test suite after each task, not only at the end of a phase.
14. Do not mark a phase complete until its exit criteria (Part I) pass, demonstrably.
15. Keep changes scoped to the task. Do not refactor unrelated code while implementing a feature.
16. Do not implement roadmap features (PRD §29–34) early, even partially, even as a "hook for later."
17. Never present an estimated or projected value as if it were measured. Label every number in the UI and API as one of: measured, estimated, projected, or realized (Part G.5).
18. Never claim or imply model-quality equivalence. Fluxen V1 does not evaluate output quality — any model-cost recommendation must say so explicitly in its evidence.
19. Every operation that changes production behavior (policy apply, revert) requires an explicit human confirmation step in the API and UI. No autonomous mutation, ever.
20. Preserve the application's ability to bypass Fluxen if the gateway becomes unavailable — this means Fluxen must never require an SDK fork or a proprietary client; it must always be reachable by pointing a stock OpenAI/Gemini/Ollama client at a different `base_url`, so reverting to that client's original `base_url` is always a one-line change for the customer.

---

# Part G — Optimization System Specification

## G.1 Opportunity lifecycle (frozen)

```text
        ┌─────────────────────────────────────────────────────┐
        │                                                       │
open ──▶│ reviewed ──▶ simulated ──▶ applied ──▶ measured        │
  │      │    │            │            │            │           │
  │      │    │            │            │            └─▶ (final state)
  │      │    │            │            └─▶ reverted (if regressed or user choice)
  │      │    │            └─▶ dismissed
  │      │    └─▶ dismissed
  │      └─▶ dismissed
  └─▶ stale (evidence no longer holds on re-detection; auto-transition)
```

State definitions:

- **open** — detector found it, nobody has looked yet.
- **reviewed** — a user opened the detail page (auto-transition on first view; also settable explicitly via `POST /review`).
- **simulated** — at least one simulation exists for this opportunity.
- **applied** — the recommended (or edited) policy change has been confirmed and written.
- **measured** — the measurement has reached `final` status (14 days post-apply) and has a verdict.
- **dismissed** — user explicitly rejected it; requires a `dismiss_reason`. Will not re-open unless evidence materially changes (new fingerprint).
- **stale** — the next detector run no longer finds supporting evidence; auto-transition, no user action. A stale opportunity is not deleted, it is archived with its last evidence for audit.
- **reverted** — user (or the regression path) rolled back an applied change; the opportunity and its measurement both record this. A reverted opportunity may re-open on next detection if the underlying inefficiency still exists.
- **failed** — reserved for an apply that errored before completing (invalid policy document, DB failure mid-transaction). Not a normal-path state; surfaced as an error, retriable.

Every transition is timestamped, and every transition away from `open`/`reviewed`/`simulated` records who did it and why (`dismiss_reason` for dismissal, the `simulation_id` for apply, `verdict_reason` for the measured outcome).

## G.2 Suppression and trust rules (frozen, minimally configurable)

Hard floors — **not** overridable per-detector, only the global multiplier is a Settings-level knob (default 1.0, range 0.5–2.0, for organizations that want stricter/looser feeds):

```text
minimum_app_requests_14d      = 1,000
minimum_app_spend_14d_micro   = 5,000,000        ($5)
minimum_savings_monthly_micro = 10,000,000       ($10)
minimum_savings_pct           = 0.05             (5% of app spend)
max_open_opportunities_per_app = 5               (ranked by savings × confidence, rest suppressed)
```

An application below the traffic/spend floor produces **no opportunities and shows "Not enough data yet"** rather than a low-confidence guess. An opportunity below the savings floor is computed internally (for the self-check tests) but never written to the `opportunities` table. The product's default output on a well-optimized, low-traffic, or already-efficient application is the sentence **"No meaningful optimization found"** — this is a correct, expected, and displayed state, not an error.

## G.3 The four detectors (frozen specification)

### G.3.1 Model cost opportunity

- **Window:** trailing 14 days. **Grain:** (application, requested_model).
- **Eligibility predicate** for candidate model `C` (a request is "eligible" for the cheaper candidate if all hold):
  ```
  input_tokens  ≤ 0.6 × C.context_window
  output_tokens ≤ min(C.max_output, p90(output_tokens for this model))
  NOT has_tool_calls              (multi-turn tool use excluded — highest regression risk)
  has_tools    ⟹ C supports tools
  has_images   ⟹ C supports vision
  json_mode    ⟹ C supports json_schema
  ```
- Candidates come only from the catalog's declared `downgrade_candidates_for` list (Part H) — never invented at detection time.
- `eligible_fraction = eligible / total`; **report only if ≥ 0.30**.
- Cost is recomputed **per eligible request** with the candidate's real prices (never a blended average).
- Confidence: `high` if `eligible_fraction ≥ 0.6` and sample ≥ 5,000 and same-provider; `medium` if `≥ 0.4` and sample ≥ 1,000; else `low`. Cross-provider candidates cap at `medium`.
- **Recommendation is a conservative split**: propose `min(eligible_fraction, 0.5)` traffic weight to the candidate, never a full replacement.
- Evidence must include: total/eligible requests, eligible fraction, exclusion breakdown by reason, token distribution (p50/p95 in/out), current vs candidate model, price ratio, and this fixed caveat string: *"Fluxen does not evaluate output quality. Review the simulation, then consider applying to a portion of traffic and comparing results."*

### G.3.2 Repeated request opportunity (exact caching only)

- **Cache key:** `sha256(app_id ‖ namespace ‖ canonical_json({model, messages, tools, tool_choice, response_format, temperature, top_p, max_tokens, stop, seed, n}))`. Computed and stored for **every** request regardless of whether caching is enabled — this is what lets the detector prove value before the feature is turned on.
- **Window:** trailing 7 days. Group by `cache_key`, count duplicates.
- Only duplicates whose inter-arrival gap is within the proposed TTL sweep count as "would have been cached" — a repeat six days later would not hit a 1-hour cache.
- TTL sweep evaluated at 5m / 1h / 6h / 24h; the evidence shows the savings curve across all four so the user can choose.
- `savings = duplicate_cost_in_ttl × 0.9` (10% haircut for real-world eviction/cold-start effects), normalized to 30 days.
- **Minimum threshold:** `duplicate_rate ≥ 0.05` in addition to the global floors (G.2).
- Evidence: duplicate rate, TTL sweep table, top duplicated request "shapes" identified by `system_prompt_hash` + token profile (never raw prompt text unless body capture is separately enabled).

### G.3.3 Token efficiency opportunity

- **Baseline:** trailing 28 days ending 7 days ago. **Current:** trailing 7 days. Segmented by model (a model change is a confound, not a token-drift signal).
- `drift = (mean(current) − mean(baseline)) / mean(baseline)`, computed separately for input and output tokens.
- Report input drift if `drift ≥ 0.25`; output drift if `drift ≥ 0.35`. Both also require ≥ 500 requests in each window and cost impact above the global savings floor.
- `cost_impact = (current_mean − baseline_mean) × current_requests_30d × input_price`.
- Include a simple **change-point estimate**: first day where a cumulative-deviation statistic crosses a threshold, shown as a marker on the trend chart, so the user has a date to investigate — not a rewritten prompt (V1 never rewrites prompts).
- Evidence: daily mean-token sparkline, drift %, cost impact, change-point date.

### G.3.4 Traffic anomaly

- Hourly series per app: request count, token volume, cost, error rate.
- Seasonal-adjusted robust z-score: median/MAD computed per hour-of-week over a 28-day lookback (minimum 14 days of history required to run at all).
- Flag when `|z| > 3.5` for 2 consecutive hourly buckets.
- Severity `high` if cost z > 5 or error-rate spike > 10 percentage points; else `medium`.
- **No savings number is attached to an anomaly.** Its action is "investigate," deep-linking to the Requests view filtered to the anomalous window. It auto-transitions to `stale` after 48h of returning to normal.
- Anomalies are the only opportunity kind that triggers a notification (webhook/Slack), when configured — the other three kinds never page anyone.

## G.4 Simulation (frozen specification)

Three scenario types only:

1. **Model mix** — reroute a percentage of a model's traffic to a candidate model; recompute cost per request using the candidate's prices against the *same recorded token counts*. Assumption stated verbatim in every result: *"Token counts are assumed unchanged between models. Actual results may differ by roughly ±15%."*
2. **Exact caching** — replay requests chronologically through a simulated cache (same key function, given TTL and size cap) to produce a true replayed hit rate, not a formula-based estimate.
3. **Budget impact** — replay chronologically, accumulate spend, mark requests that would have been blocked past the limit. Output is framed as **impact** ("N requests, X% of traffic, would have been rejected, concentrated on dates D1–D2"), never as "savings" — a budget is a control, not an optimization.

Simulation always:

- runs against real historical request facts for the application (never synthetic data, never production-affecting),
- calls the exact same `pkg/policy.Evaluate` and `pkg/pricing.Calculate` functions the gateway uses,
- reports: current cost, simulated cost, delta ($ and %), affected request count, per-model breakdown, and an explicit assumptions list,
- is capped at 2M replayed requests per run; beyond that it samples uniformly and labels the result as sampled.

**Self-check (release-blocking test):** replaying the *currently applied* policy over a past window must reproduce actual recorded cost within 0.5%. This is the credibility floor for the whole feature — see Part K.

No quality simulation exists or is implied anywhere in the UI or API.

## G.5 Apply and Measurement (frozen specification)

**Apply** (`POST /api/v1/opportunities/{id}/apply`) is one transaction:

1. Validate the target `PolicyDocument`.
2. Write `policies` (version+1) and `policy_history` (`change_source="opportunity"`, linked `opportunity_id`/`simulation_id`).
3. Transition the opportunity to `applied`.
4. Freeze the baseline: a `measurements` row capturing the 14 days immediately preceding `applied_at`, with `baseline_cost_per_1k_micro` computed and stored **at apply time** — later retention deletion or catalog changes cannot retroactively alter it.
5. Schedule interim (+7d) and final (+14d) measurement checks.

Requires `confirm: true` in the request body and, in the UI, the user typing the application's slug for any budget or routing change (irreversible-feeling actions get friction on purpose).

**Measurement** compares **cost per 1,000 requests**, not raw totals, because request volume always changes:

```
actual_savings = (baseline_cost_per_1k − observed_cost_per_1k) × observed_requests / 1000
actual_pct     = 1 − observed_cost_per_1k / baseline_cost_per_1k
```

Verdicts:

| Verdict | Condition |
|---|---|
| `successful` | `actual_pct ≥ 0.7 × expected_pct` |
| `partial` | `0.2 × expected_pct ≤ actual_pct < 0.7 × expected_pct` |
| `no_effect` | `\|actual_pct\| < 0.02` |
| `regressed` | `actual_pct < −0.02` |
| `inconclusive` | observed requests < 30% of baseline requests, OR a second policy change landed inside the measurement window (detected via `policy_history`) |

A `regressed` verdict always surfaces a one-click **Revert**, which restores the prior `policy_history` document and marks the opportunity `reverted`. Realized savings shown anywhere in the product (Overview included) sums only `actual_savings_micro` from `successful`/`partial` measurements plus exact-cache `cache_saved_micro` — **realized savings is never an estimate**, and estimated/projected numbers are never added into it.

## G.6 Value labeling (frozen — applies everywhere in UI and API)

Every number is exactly one of:

- **Measured** — recorded from real traffic (spend, tokens, requests, realized savings).
- **Estimated** — a detector's or simulator's calculation from historical facts (potential savings, projected cost, simulation results).
- **Projected** — a forward-looking normalization (e.g., a 7-day window's savings scaled to 30 days).
- **Realized** — the outcome of a completed, verified measurement.

The UI marks non-measured values with a visible "est." affordance (§Part H, dashboard conventions). The API includes a `value_type` field on every monetary figure in opportunity, simulation, and measurement responses.

---

# Part H — Cost Intelligence

## H.1 Pricing catalog

`pkg/pricing/catalog.yaml`, embedded in the binary, versioned by date string, human-editable:

```yaml
version: "2026-09-01"
models:
  - id: openai/gpt-4o
    provider: openai
    aliases: [gpt-4o, gpt-4o-2024-11-20]
    input_per_mtok_micro: 2500000
    output_per_mtok_micro: 10000000
    context_window: 128000
    max_output: 16384
    capabilities: [tools, vision, json_schema, streaming]
    tier: premium
  - id: openai/gpt-4o-mini
    provider: openai
    input_per_mtok_micro: 150000
    output_per_mtok_micro: 600000
    context_window: 128000
    capabilities: [tools, vision, json_schema, streaming]
    tier: economy
    downgrade_candidates_for: [openai/gpt-4o]
```

`downgrade_candidates_for` is the *only* source of candidates for the model-cost detector — it is data, not a runtime heuristic. Adding a new candidate pair is a catalog edit, not a code change.

## H.2 Rules (frozen)

- Every cost figure is integer micro-USD (`bigint`), computed once, never recomputed from a rounded display value.
- Every `requests` row stores the `pricing_version` used. Catalog updates do not rewrite history; a rollup **backfill** is an explicit, logged operator action, never automatic.
- Unknown model → `cost_status = unknown`, `cost_micro = 0`, excluded from all savings math (Part D.2).
- Ollama → `cost_status = local`, `cost_micro = 0`, visually and numerically excluded from billed-cost totals unless a view explicitly labels it "local" (Part D.1).
- No GPU/electricity cost model in V1.

---

# Part I — Dashboard Specification

Applies to `web/apps/dashboard`. This section specifies each screen's full target behavior; it is not built all at once. Part L's phased plan is now organized around the product's Aha Moment (Phase 3), not around completing screens independently — so the *build order* is: enough of Application Detail to show real usage/cost (Phase 2) → the Optimizations detail page's Why/Evidence/Impact sections against real detected traffic (Phase 3, the Aha Moment) → Simulate (Phase 4) → Policies editor + Apply (Phase 5) → Measure (Phase 6) → Overview, Requests, Policies matrix, Settings, and full polish (Phase 7). A screen's full spec below may therefore be implemented incrementally across phases; each phase in Part L states exactly which slice it needs.

For every screen below: purpose, route, data, actions, and states are specified. "States" always means: loading, empty, not-enough-data (where applicable), error, success.

## I.1 Application Detail — `/apps/[appId]`

**Purpose:** the single most important screen — the AI Efficiency Profile for one application (PRD §18).

**Layout:** persistent header (name, status pill: `receiving traffic` / `idle 3d+` / `never connected`, 30-day spend, efficiency ring, open-opportunity value) + tabs: Usage & Cost → Models → Efficiency → Opportunities → Policies → Requests → Keys.

**Data:** `GET /api/v1/applications/{id}/summary`, `.../timeseries`, `.../models`, `.../score`, `GET /api/v1/opportunities?app_id=`, `GET /api/v1/applications/{id}/policy`.

**Actions:** switch tabs (URL-synced), change time range (global picker, URL-synced), click an opportunity → Optimizations detail, click "Copy connection details" (Keys tab) → connect snippet with `base_url` + key.

**States:**
- *Never connected* (no requests ever): header shows "Not connected yet" and the connect snippet inline, no charts.
- *Idle* (had traffic, none in 24h): banner "No traffic in the last 24 hours," data still shown.
- *Not enough data* (< 1,000 requests / 14d): efficiency ring shows "—" with tooltip "Not enough data yet," Opportunities tab shows "No meaningful optimization found — check back as traffic grows."
- *Error*: API failure shows a retry affordance per section (independent — one failed panel doesn't blank the page).
- *Loading*: skeleton per section, not a full-page spinner.

## I.2 Optimizations — `/optimizations` (list) and `/optimizations/[id]` (detail)

**Purpose:** the central opportunity feed; the detail page is the core Fluxen UX.

**List route data:** `GET /api/v1/opportunities?status=&kind=&app_id=&sort=savings`. Filters: status, kind, application. Default sort: `savings_micro × confidence_score` descending.

**List actions:** click row → detail. Bulk dismiss is not in V1 (one at a time, deliberately — dismissal is a decision, not housekeeping).

**Detail route** strictly follows: **Why → Evidence → Impact → Simulate → Apply → Measure**.

- *Why* — `title` + `summary`, one sentence, plain language.
- *Evidence* — kind-specific panel rendering the `evidence` JSON (eligibility breakdown bar for model cost; TTL sweep table for repeated request; token trend + change-point marker for token efficiency; anomaly timeline for traffic anomaly). Always includes the confidence badge with its tooltip explaining the score.
- *Impact* — current cost, projected cost, savings ($ and %), all labeled "estimated" (Part G.6).
- *Simulate* — button opens the scenario form pre-filled from `recommendation`; disabled state explains why if the app has too little data.
- *Apply* — disabled until at least one simulation exists for this opportunity; opens the confirm dialog (slug re-typed for routing/budget changes).
- *Measure* — appears only once `status = applied`; shows collecting/interim/final state and, once final, the before/after verdict block with Revert if `regressed`.

**States:** loading (skeleton per section), error (per-section retry), and for *dismissed*/*stale* opportunities the detail page renders read-only with a banner explaining why ("Dismissed by {user} on {date}: {reason}" / "Evidence no longer holds as of {date}").

## I.3 Overview — `/`

**Purpose:** organization-wide entry point.

**Data:** `GET /api/v1/overview?range=30d`, `.../timeseries`.

**Layout:** spend/requests/tokens header with sparkline + WoW delta → potential savings tile (estimated) + realized savings tile (realized, Part G.6) → provider mix donut → cost-over-time stacked by provider → top applications table (spend, Δ, efficiency, opportunity value) → opportunity feed (top 5 by savings × confidence, each with a Review button linking to I.2).

**Actions:** change range, click an application row → Application Detail, click an opportunity card → Optimizations detail.

**States:** *empty* (zero applications) shows the setup wizard entry point instead of empty charts; *not enough data* for the whole org shows the header numbers as available and a message in place of the opportunity feed ("Opportunities will appear once your applications have enough traffic").

## I.4 Policies — `/policies` (matrix) and per-app editor (inside Application Detail's Policies tab)

**Purpose:** cross-app view of controls, and the editor for one app's policy.

**Matrix data:** `GET /api/v1/applications` (includes policy summary per app). One row per app, one compact state chip per control (routing / cache / budget / rate limit / model restrictions).

**Editor:** loads `GET /api/v1/applications/{id}/policy`, edits the five controls (Part G.7 below), shows a diff before save, requires an optional note, submits `PUT .../policy`.

**Actions:** toggle/edit a control, preview diff, save (writes `policy_history`), view history (`GET .../policy/history`), revert to a prior version.

**States:** *loading*, *saving* (optimistic UI disabled — policy changes wait for server confirmation given the hot-path implications), *error* (validation errors shown inline per field, e.g. "Budget limit must be greater than $0"), *success* (toast + updated chip).

## I.5 Requests — `/requests`

**Purpose:** investigation only. Explicitly **not** a tracing platform — no span trees, no waterfalls.

**Data:** `GET /api/v1/requests?app_id=&model=&status=&cache=&from=&to=&cursor=` (cursor-paginated, virtualized table).

**Columns:** time, app, requested→served model, tokens, cost (with value-type label), latency, cache status, status.

**Actions:** filter, open detail drawer (`GET /api/v1/requests/{id}`) showing full metadata, routing decision, policy version at request time, and request/response bodies only if capture was enabled for that app.

**States:** *empty* (no requests match filters) with a "clear filters" affordance; *no traffic yet* for the app entirely, links to the connect snippet.

## I.6 Settings — `/settings/*`

Functional-minimum: Providers (credentials CRUD + health check), Pricing (view catalog, view/edit org overrides, view Ollama local-cost label — no GPU cost fields), Users (owner/member list, invite), Retention (the three retention knobs from Part E.2). No polish pass until Phase 7.

## I.7 Cross-cutting UI conventions

- All money renders through one `<Money value={micro} status={valueType} />` component. No float math anywhere in TSX.
- Global time-range picker syncs to the URL.
- One shared colorblind-safe categorical palette for all charts.
- Dark and light themes both first-class from the start (not retrofitted).
- Value-type labeling (Part G.6) is a shared `<ValueBadge type="estimated|measured|projected|realized" />` used everywhere a number appears.

---

# Part J — First-Run Experience (Definition of Done source)

```text
docker compose up
      ↓
open dashboard → setup wizard (no accounts exist yet)
      ↓
create owner account (email + password)
      ↓
create first application (name → slug auto-generated, editable)
      ↓
create API key for that application (shown once, copyable)
      ↓
configure at least one provider credential (OpenAI key, or Gemini key, or
      Ollama base URL — wizard accepts any one to proceed)
      ↓
connect screen shows the exact base_url + key + a copy-paste snippet
      (OpenAI Python SDK, OpenAI Node SDK, curl)
      ↓
user changes their application's base_url and sends one real request
      ↓
gateway proxies it; dashboard shows the request within seconds
      ↓
Application Detail shows usage, cost, model/provider mix as traffic accrues
      ↓
once traffic crosses the minimum threshold (Part G.2), a detector produces
      a real opportunity with evidence
```

**Target:** a person who has never seen Fluxen reaches step "receive a meaningful opportunity" in approximately 15 minutes on their own infrastructure, using only the README and in-product wizard — no external help. This is the single acceptance test for the entire V1 build (Part L).

---

# Part K — Testing Strategy

| Layer | Scope | Tooling |
|---|---|---|
| Unit | pricing math, cache key canonicalization, policy evaluation, detector algorithms on fixtures, verdict logic, score components | `go test` |
| Golden | provider request/response translation, both directions, for all three providers | recorded payloads in `pkg/providers/*/testdata` |
| Integration | full gateway pipeline against a mock provider server, real Postgres/Redis (testcontainers) | `test/integration` |
| Contract | generated TS client vs `api/openapi.yaml` vs Go handlers stay in sync | schema diff check in CI |
| E2E | the full journey in Part J plus apply → measure → revert | Playwright |
| Determinism (release-blocking) | replaying the currently-applied policy over a past window reproduces actual cost within 0.5% | `sim_test.go`, also exposed as `fluxenctl detect --self-check` |
| Detector quality (release-blocking) | five labelled synthetic-traffic fixtures: clean, expensive-model, duplicate-heavy, token-growth, spiky/anomalous. Each detector must fire on its target fixture and **the clean fixture must produce zero opportunities across all four detectors.** | `test/golden/traffic_fixtures/` + `detect` test suite |

CI fails the build on: any release-blocking test failure, any OpenAPI/client contract drift, and any detector false positive on the clean-traffic fixture.

---

# Part L — Phased Implementation Plan

## L.0 Product-first sequencing principle

This plan is **not** organized by technical layer or by "complete each subsystem, then move on." It is organized around progressively proving one product hypothesis:

> **Fluxen can observe an application's AI traffic, discover meaningful inefficiency, explain it, estimate its impact, let the user simulate it, allow controlled application, and measure the real result.**

The first usable vertical slice matters more than completing every backend subsystem independently. Concretely, this means:

- Gemini and Ollama — full providers in the previous plan's Phase 3 — are now **deferred to Phase 7**. OpenAI alone is enough to prove the hypothesis, and multi-provider breadth adds nothing to whether the Aha Moment is credible.
- The dashboard is built in **thin vertical slices matched to what each phase needs to demonstrate**, not screen-by-screen. Overview, the Policies matrix, and full Settings — none of which are needed to reach or prove the Aha Moment — are pushed to Phase 7.
- **Phase 3 is the single highest-priority phase in this document.** Every phase before it exists only to make Phase 3 possible on real traffic; every phase after it exists to make the moment Phase 3 proves become safe to act on and honest about its results.
- Do not build ahead of the current phase's needs. If a task in a later phase isn't required to reach the current phase's exit criteria, it does not belong in the current phase, however natural it would be to build alongside related code.

## L.1 Phase map

| Phase | Type | Name | Proves |
|---|---|---|---|
| 0 | Foundation | Foundation | The stack runs |
| 1 | Foundation | First AI Request | Fluxen can carry real traffic |
| 2 | Foundation | First Understanding | Fluxen can explain what an app is doing |
| 3 | **⭐ Product milestone (Aha Moment)** | **Opportunity Discovery** | **Fluxen finds something real and explains why** |
| 4 | Product milestone | Prove It (Simulate) | The recommendation is trustworthy before acting |
| 5 | Product milestone | Control It (Apply) | The user can safely act on it |
| 6 | Product milestone | Measure It | Fluxen is honest about whether it worked |
| 7 | Productization | Complete V1 Product | Everything else the PRD requires, safely, last |

Phases 0–2 are load-bearing infrastructure with no product payoff of their own — they exist solely so Phase 3 can run against real, attributed, priced traffic. Phases 4–6 are the parts of the product loop (PRD §14, "measure the result") that turn the Aha Moment from a demo into something a customer keeps using. Phase 7 is explicitly last: it adds breadth (more providers, more screens, more polish) only after the core loop is proven, never before.

---

## Phase 0 — Foundation

**Type:** Foundation

**Product goal:** None — this phase has no user-facing product goal. It exists to make every later phase possible.

**User outcome:** None yet. No user-facing behavior ships in this phase.

**Technical objective:** A running, empty skeleton — repository structure, local dev loop, and the datastores every later phase depends on, all provably alive together.

**Dependencies:** None.

**Backend tasks:**
- Go module + `cmd/fluxen` entrypoint with `/healthz`, `/readyz`.
- `internal/config`: env-based config, validated at boot.
- Postgres connection + `fluxenctl migrate up/down` (goose).
- Redis connection + health check.
- Structured `slog` logging; `/metrics` Prometheus endpoint (process-uptime gauge is enough for now).
- Minimal authentication/setup foundation: password hashing utility and session-cookie scaffolding wired but not yet exposed via any real endpoint (Phase 1 uses it).

**Frontend tasks:**
- `web/` pnpm workspace scaffold: `apps/dashboard`, `apps/website`, each a minimal Next.js app rendering a placeholder page.

**Database tasks:**
- `db/migrations/00001_init.sql`: `organizations`, `users` (schema only).

**API contracts:**
- `/healthz`, `/readyz`, `/metrics` only. No product API yet.

**Tests:**
- Config validation unit tests (missing required var fails boot; valid config boots).
- Boot smoke test against testcontainer Postgres/Redis, asserting `/readyz` reflects both.

**Acceptance criteria:** `docker compose up` brings up postgres, redis, fluxen, dashboard, all reporting healthy; migrations apply cleanly; `go test ./...` and `pnpm build` both pass in CI.

**Exit criteria:**
```text
docker compose up
→ all services healthy
→ frontend reachable, backend reachable
→ database migrations work
→ Redis works
→ tests pass
```

**Explicit non-goals:** No auth flow exposed via API yet. No application concept. No gateway. No business logic of any kind.

**Demo/test scenario proving the phase works:** Clone the repo on a clean machine, run `docker compose up`, hit `curl localhost:8080/readyz` and get `200 {"postgres":"ok","redis":"ok"}`, open the dashboard placeholder page in a browser.

---

## Phase 1 — First AI Request

**Type:** Foundation

**Product goal:** A developer can connect an application and successfully send AI traffic through Fluxen.

**User outcome:** "I pointed my existing OpenAI client at Fluxen and it just worked — same responses, same streaming, nothing broke."

**Technical objective:** Prove the minimum end-to-end path: `Application → Fluxen Gateway → Provider → Response`, for OpenAI only, with every request captured as a durable, attributed fact.

**Dependencies:** Phase 0 complete.

**Backend tasks:**
- `pkg/types`: `CanonicalRequest`, `CanonicalResponse`, `UsageRecord` (Part C.3).
- Application + API key schema and minimal CRUD (enough to create one application and one key — full application lifecycle UI comes in Phase 2/7, but the underlying create/list/revoke API is built now because the gateway needs it).
- `internal/auth`: key resolver (`fx_` prefix → `AppID`, in-process LRU → Postgres), session auth for the one setup/login flow needed to create that first application.
- Minimal setup flow: `POST /api/v1/setup` (create org + owner, once), login/logout.
- `pkg/providers/openai`: client, request/response translation preserving unknown fields, streaming with `stream_options.include_usage` injection, error normalization.
- `internal/gateway`: auth middleware, `handler_openai_chat.go`, pipeline (stages: authenticate → parse → invoke → account → emit → respond — policy/cache/routing stages are no-ops, wired for real in Phase 5).
- `internal/gateway/stream.go`: tee-and-flush streaming, client-abort handling producing a `status=client_abort` usage record.
- `pkg/pricing`: catalog loader, `Calculate()`, unknown-model → `cost_status=unknown` (never fabricated).
- `internal/ingest`: bounded async queue + batch writer into `requests` (hot-path contract, Part B.2 — analytics failure must never fail a request).
- Request IDs (`X-Fluxen-Request-Id`) generated, returned, and persisted.
- Upstream timeout handling (default 120s) → clean `504`/`upstream_timeout`.

**Frontend tasks:**
- Setup wizard (owner creation), login page.
- Application create form + list (minimal — full Application Detail is Phase 2).
- API key issuance screen showing the raw key exactly once, plus a connect snippet (`base_url` + key, OpenAI Python/Node/curl).

**Database tasks:**
- `db/migrations/00002_applications.sql`: `applications`, `api_keys`.
- `db/migrations/00003_requests.sql`: partitioned `requests` table (Part E.1).

**API contracts:**
- `POST /api/v1/setup`, `POST/GET /api/v1/auth/*`
- `POST/GET /api/v1/applications`, `POST /api/v1/applications/{id}/keys`, `DELETE /api/v1/keys/{id}`
- Gateway: `POST /v1/chat/completions` (OpenAI-compatible)

**Tests:**
- Unit: types round-trip, key hashing/session issuance.
- Golden: OpenAI request/response translation (chat, streaming, one error case).
- Integration: full pipeline incl. streaming and client-abort; revoked key gets 401.
- Load smoke (informational only, not release-blocking yet): added p50 latency for a non-streaming call.

**Acceptance criteria:** An unmodified `openai` Python or Node SDK, pointed at Fluxen via `base_url`/`api_key` only, successfully completes both a streaming and a non-streaming chat request, and every such request lands in `requests` with correct token counts, correct app attribution, and a request ID that matches what the client received.

**Exit criteria:**
```text
A real OpenAI-compatible application works through Fluxen:
Application → Fluxen Gateway → Provider → Response, proven for both
streaming and non-streaming traffic, with every request persisted and
attributed to the correct application.
```

**Explicit non-goals:** No Gemini, no Ollama. No caching, rate limiting, budgets, or routing (pipeline stages are stubs). No dashboard analytics beyond seeing the raw key and connect snippet. No detectors.

**Demo/test scenario proving the phase works:** Create an application in the UI, copy the issued key, run `openai.ChatCompletion.create(...)` against `http://localhost:8080/v1` with that key from a plain Python script (both `stream=True` and `stream=False`), and confirm a row appears in `requests` with the correct app_id, token counts, and cost.

---

## Phase 2 — First Understanding

**Type:** Foundation

**Product goal:** Fluxen can understand the application's AI traffic well enough to show it back to the user.

**User outcome:** "I can see what my application is actually doing and spending — requests, tokens, cost, which model, how fast, how often it errors."

**Technical objective:** Turn raw `requests` rows into trustworthy, aggregated, application-scoped analytics, and surface them in a real (if minimal) Application Detail view. This phase's entire purpose is to give Phase 3's detectors something real to analyze and give the user something real to look at when Phase 3 finds something.

**Dependencies:** Phase 1 complete — real traffic must already be flowing and persisted.

**Backend tasks:**
- `internal/worker`: real in-process ticker scheduler (Part C.7), replacing any Phase 1 stub.
- `internal/rollup`: hourly job (requests → hourly), daily job (hourly → daily + `application_daily`), each idempotent and re-runnable for a window.
- `GET /api/v1/applications/{id}/summary`, `.../timeseries`, `.../models`: cost/usage/latency/error breakdown by provider and model.
- `tools/trafficgen`: demo/realistic traffic generator producing multi-model, multi-day traffic with **deliberately injected inefficiency** (an over-provisioned model on a meaningful fraction of requests) — built now, not later, because Phase 3 needs a reliable fixture to detect against and a fresh install needs a way to demonstrate the Aha Moment without days of real waiting.
- OpenAPI contract wiring for everything shipped so far; contract-drift check added to CI.

**Frontend tasks:**
- Application Detail — Usage & Cost tab and Models tab, wired to real data (Part I.1, partial: Efficiency/Opportunities/Policies tabs stay placeholders until Phases 3/5).
- Applications list — minimal table (name, spend, requests) sufficient to navigate to Application Detail; the full Efficiency/Opportunity columns from Part I's spec arrive with Phase 3/6.

**Database tasks:**
- `db/migrations/00004_rollups.sql`: `request_rollup_hourly`, `request_rollup_daily`, `application_daily`.

**API contracts:**
- `GET /api/v1/applications/{id}/summary`, `/timeseries`, `/models`

**Tests:**
- Unit: rollup correctness and idempotency (re-running a window never double-counts).
- Integration: summary/timeseries/models endpoints against seeded rollups, covering multiple models in one window.
- `fluxenctl seed` produces a working demo dataset end-to-end.

**Acceptance criteria:** Every number shown in Application Detail traces to a rollup that traces to real `requests` rows, correctly attributed by model, with no double-counting under repeated rollup runs.

**Exit criteria:**
```text
The user can see what their application is actually doing and spending:
requests, tokens, cost, model/provider mix, latency, and errors, for a
real (or seeded) application, in the dashboard.
```

**Explicit non-goals:** No efficiency score, no opportunities, no Overview page, no Requests investigation screen, no Policies. This phase is observation only — it does not yet tell the user anything is wrong, only what is happening.

**Demo/test scenario proving the phase works:** Run `fluxenctl seed --demo`, open Application Detail for the seeded `document-ai` application, and see correct 30-day spend, request count, and a model-mix breakdown that matches what the seeder actually generated.

---

## Phase 3 — ⭐ AHA MOMENT — Opportunity Discovery

**Type:** ⭐ Product milestone — the Aha Moment. **Highest product priority in this document.**

**Product goal:** Fluxen identifies a credible optimization opportunity from the application's own real traffic, and explains it well enough that the user believes it.

**User outcome:** *"Fluxen looked at MY application's AI traffic and found something I can actually optimize."* Concretely — the user opens their application (or the demo application) and sees:

```text
Application: document-ai

Current spend: $1,240/month
Requests: 182,421
Potential savings: $310/month

Recommendation:
61% of recent requests appear suitable for a lower-cost model.

Current:            Premium Model
Suggested:           Lower-cost Model
Potential savings:   $310/month
Confidence:          High

Actions: [Review]  [Simulate]
```

**Technical objective:** Implement the minimum *complete* optimization loop — `Traffic → Analyze → Detect opportunity → Explain why → Estimate impact → Show recommendation` — for exactly one detector, run against real observed traffic, not static or hardcoded demo data.

**Which detector:** the **Model Cost Opportunity** detector (Part G.3.1). It is the most credible V1 opportunity: it requires no caching infrastructure (unlike Repeated Request), no multi-week baseline (unlike Token Efficiency), and produces a concrete, explainable dollar figure (unlike Traffic Anomaly, which has no savings number at all). It is also the exact shape of the PRD's own worked example. The other three detectors are correctly deferred to Phase 7 (Part L.7) — they are real V1 features, but none of them is required to prove the hypothesis, and building all four before proving one would delay the Aha Moment for no product reason.

**Dependencies:** Phase 2 complete — needs real, rolled-up, attributed traffic with per-request workload features (`has_tools`, `has_tool_calls`, `has_images`, `json_mode`, token counts) already being captured since Phase 1.

**Backend tasks:**
- `db/migrations/00005_opportunities.sql`: `opportunities` table (Part E.1) — `efficiency_scores` deferred to Phase 7, it is not needed to show one opportunity.
- `internal/detect`: `Detector` interface, registry, runner with the global suppression rules (Part G.2 — minimum traffic/spend, minimum savings, ranked by savings × confidence). These floors exist starting now, not later — a noisy first opportunity would poison the Aha Moment rather than deliver it.
- Model cost detector (Part G.3.1) in full: eligibility predicate, per-request recosting against the candidate model's real catalog price (never a blended average), exclusion-reason breakdown, confidence tiering, the conservative-split recommendation (`min(eligible_fraction, 0.5)`), and the fixed no-quality-claim caveat string (Rule 18, Part F).
- Fingerprinting/dedupe: re-running the detector against unchanged traffic must not create a duplicate opportunity.
- `GET /api/v1/opportunities?status=&app_id=`, `GET /api/v1/opportunities/{id}`, `POST /api/v1/opportunities/{id}/review`.
- `tools/trafficgen`: extend the Phase 2 fixture (or confirm it already does) so a fresh `fluxenctl seed --demo` produces traffic that crosses the detector's minimum-traffic floor immediately — a fresh install must not need to wait days to see the Aha Moment.

**Frontend tasks:**
- Optimizations detail page — **Why, Evidence, and Impact sections only** (Part I.2's `Simulate`/`Apply`/`Measure` sections render as visibly disabled/"coming in a later step," not built yet — Phase 4 builds Simulate for real).
- Evidence panel for the model-cost kind specifically: eligibility breakdown, token distribution, current vs candidate model, price ratio, confidence badge with tooltip, and the caveat text — rendered from the real `evidence` JSON, not mocked.
- Minimal Optimizations list (`/optimizations`) — enough to reach the detail page; full filter/sort UX from Part I.2 can wait for Phase 7 if time-constrained, but the list itself must show real opportunities.
- A visible entry point from Application Detail (a card or banner: "1 opportunity found") — the Efficiency tab placeholder from Phase 2 can now show at minimum an opportunity count; the full efficiency *score* is still Phase 7.
- `Review` action wired (`POST .../review`); `Simulate` button is present but disabled with a tooltip ("Simulation arrives in the next step") until Phase 4 ships.

**Database tasks:**
- `opportunities` (Part E.1 fields, minus nothing — the full schema is needed from the start since `recommendation` must already be shaped for Phase 5's apply to consume later).

**API contracts:**
- `GET /api/v1/opportunities`, `GET /api/v1/opportunities/{id}`, `POST /api/v1/opportunities/{id}/review`

**Tests:**
- Unit: the eligibility predicate, confidence tiering, and conservative-split recommendation math (Part G.3.1), each independently verifiable against a hand-computed fixture.
- **Release-blocking detector-quality test, introduced now:** an "expensive-model" synthetic fixture must produce exactly the expected opportunity; a "clean" fixture (no injected inefficiency) must produce **zero** opportunities. This is the first entry in what Part K calls the detector-quality suite — the other four fixtures arrive with their detectors in Phase 7.
- Integration: opportunity lifecycle `open → reviewed`, dedupe on re-run.

**Acceptance criteria:** Running the detector against `trafficgen`'s expensive-model fixture produces one opportunity whose evidence, current cost, projected cost, savings, and confidence are all internally consistent with the fixture's known parameters; running it against clean traffic produces nothing.

**Exit criteria:**
```text
A fresh installation can send realistic traffic through Fluxen, and
Fluxen can produce at least one credible, explainable optimization
opportunity from that traffic — based on real observed traffic, not
static or hardcoded demo data — displaying application, current
behavior, recommended change, evidence, eligible traffic, current cost,
projected cost, potential savings, confidence, and assumptions/caveats.
```

**Explicit non-goals:** No repeated-request, token-efficiency, or traffic-anomaly detectors yet (Phase 7). No efficiency score. No Simulate, Apply, or Measure — the buttons exist as a preview of what's coming but do nothing yet. No Overview page, no Policies, no Gemini/Ollama. Do not build any of these before this phase's exit criteria pass — per the critical product rule, a complete Optimizations screen with only one working detector beats four half-built detectors.

**Demo/test scenario proving the phase works:** Fresh `docker compose up` → `fluxenctl seed --demo` (or real traffic sent manually via Phase 1's connect snippet, run long enough to cross the traffic floor) → open the seeded `document-ai` application in the dashboard → land on an Optimizations detail page showing a real, evidence-backed model-cost opportunity matching the shape of the PRD's own worked example, with a `Review` button that works and a `Simulate` button that visibly previews the next step.

---

## Phase 4 — Prove It (Simulate)

**Type:** Product milestone

**Product goal:** Let the user test whether the Aha Moment's recommendation would actually have made a difference, without touching production.

**User outcome:** "I don't have to trust Fluxen's number blindly — I can see, using my own real history, what would have happened."

**Technical objective:** Historical replay of real request facts through the same pricing/policy logic production uses, for the one recommendation type Phase 3 can produce (model mix), plus the caching scenario since it shares the replay engine and is needed before Phase 7's repeated-request detector arrives.

**Dependencies:** Phase 3 complete — needs a real opportunity to simulate against.

**Backend tasks:**
- `db/migrations/00006_simulations.sql`: `simulations` (Part E.1).
- `internal/sim/engine.go` + `replay.go`: bounded-memory chronological replay of `requests` for an app/window, with the 2M-row sampling cap (Part G.4).
- Model mix scenario (`modelmix.go`): recost eligible historical requests at the candidate model's price via `pkg/pricing.Calculate` — the **same function** the gateway uses (Rule 9, Part F) — never a reimplementation.
- Exact caching scenario (`caching.go`): chronological replay through a simulated LRU keyed by the real cache-key function (Part G.3.2), even though caching itself isn't enforceable in production until Phase 5 — the simulation only needs the key function and historical requests, both of which already exist.
- Budget scenario is **deferred to Phase 5**, alongside the budget control it simulates — building it here with no corresponding control to apply it to would be simulation for its own sake.
- **The self-check (release-blocking from this point forward):** replaying the currently-applied (empty/no-op) policy over a past window reproduces actual recorded cost within 0.5%. This is the credibility floor for the entire simulation feature (Part G.4/K) and must pass before Simulate ships.
- `POST /api/v1/simulations` (app_id, opportunity_id, scenario, window), `GET /api/v1/simulations/{id}`.

**Frontend tasks:**
- Simulate section of the Optimizations detail page (Part I.2), now fully functional: scenario form pre-filled from the opportunity's `recommendation`, current-vs-simulated result view, affected-request count, per-model breakdown, and the always-visible assumptions block (the ±15% tokenizer-assumption caveat from Part G.4).
- The `Simulate` button from Phase 3, previously disabled, now opens this real flow.

**Database tasks:**
- `simulations` table.

**API contracts:**
- `POST /api/v1/simulations`, `GET /api/v1/simulations/{id}`, `GET /api/v1/applications/{id}/simulations`

**Tests:**
- Unit: model-mix and caching recost correctness against hand-computed fixtures.
- **Release-blocking:** the 0.5% self-check, wired into CI as a required check from this phase onward.
- Integration: simulation create → poll → complete for both scenario types.

**Acceptance criteria:** Simulating the Phase 3 opportunity produces a result whose "current cost" matches that opportunity's own reported current cost, and whose simulated cost is internally consistent with the recommendation's proposed split.

**Exit criteria:**
```text
The user can take the Aha Moment recommendation and prove its potential
impact using their own historical traffic — current vs simulated,
estimated savings, affected request count, assumptions, and uncertainty
all shown, using the same pricing/policy logic as production.
```

**Explicit non-goals:** No budget simulation yet (arrives with budgets in Phase 5). No Apply — the simulation result is read-only; nothing in production changes. No quality simulation, ever.

**Demo/test scenario proving the phase works:** From the Phase 3 opportunity, click Simulate, accept the pre-filled 50% split, run it, and see a result page whose numbers a human can verify by hand against a handful of the underlying `requests` rows.

---

## Phase 5 — Control It (Apply)

**Type:** Product milestone

**Product goal:** Let the user safely act on the Aha Moment's recommendation.

**User outcome:** "I reviewed it, I proved it with my own data, and now I can turn it on — and Fluxen actually enforces it."

**Technical objective:** Implement the V1 control surface (Part G.7 / PRD §22) for real, and connect the Apply action to it, so that confirming an opportunity's recommendation produces a real, versioned, enforced policy change.

**Dependencies:** Phase 4 complete — Apply is gated on a simulation existing for the opportunity being applied.

**Backend tasks:**
- `pkg/policy`: `PolicyDocument` schema and the **pure** `Evaluate(doc, requestFacts, rng)` function (Part C.2 invariant — no I/O; this is the function Phase 4's simulation already calls, now also wired into the live gateway pipeline).
- `db/migrations/00007_policies.sql`: `policies`, `policy_history`.
- `internal/policy`: store, in-process snapshot with Redis pub/sub invalidation (5s TTL, Part C.5), history.
- `internal/cache` (exact caching, enforced): key canonicalization (golden-tested for stability across JSON key ordering), eligibility rules, Redis-backed store with TTL, SSE replay for cached streaming responses.
- `internal/guard`: rate limiting (Redis token bucket, Lua-atomic, **fail-open if Redis unreachable** — Rule 20) and budget enforcement (period spend counter, hard/soft, same fail-open rule).
- Model restriction enforcement (403 `model_not_allowed`, or substitute).
- Percentage routing (`internal/route`): weighted split + sticky-by-session, `route_reason`/`route_variant` recorded per request — this is what makes the model-mix recommendation from Phase 3 actually enforceable.
- Wire all five controls into the real gateway pipeline, replacing the Phase 1 no-op stages.
- Budget simulation scenario (deferred from Phase 4, built now alongside the budget control it simulates).
- `internal/policy/apply.go`: the apply transaction — validate document, write `policies`+`policy_history` (`change_source="opportunity"`, linked `opportunity_id`/`simulation_id`), transition the opportunity to `applied`, requires `confirm: true` (Rule 19, Part F — no autonomous mutation, ever).
- `POST /api/v1/opportunities/{id}/apply`, `GET/PUT /api/v1/applications/{id}/policy`, `GET .../policy/history`, `POST .../policy/revert`.

**Frontend tasks:**
- Policies editor inside Application Detail (Part I.4): the five controls, diff-before-save, optional note, writes to history.
- Apply confirmation dialog on the Optimizations detail page (Part I.2): slug re-typed for routing/budget changes, calls `POST .../apply` with `confirm: true`.
- The `Apply` button from Phase 3/4, previously disabled, now opens this real flow.

**Database tasks:**
- `policies`, `policy_history`.

**API contracts:**
- `POST /api/v1/opportunities/{id}/apply`
- `GET/PUT /api/v1/applications/{id}/policy`, `GET .../policy/history`, `POST .../policy/revert`

**Tests:**
- Unit: the pure policy evaluator, every control in isolation and combined — the highest-value suite in the codebase since Phase 4's simulation already depends on it.
- Integration: each control's live gateway effect; Redis-outage fail-open behavior; the full apply transaction including baseline handling stub (full baseline freeze lands in Phase 6, but `apply.go` must already call into it).
- Statistical test: observed routing split converges to configured weights over N requests.

**Acceptance criteria:** For each of the five controls, changing it (directly, or via applying the Phase 3 opportunity) produces an observably different gateway response within 5 seconds, visible in policy history with the correct diff.

**Exit criteria:**
```text
A user can review an optimization, create/apply a controlled policy
change, and Fluxen begins enforcing it — proven for the Aha Moment's
own model-routing recommendation, applied end to end from the
Optimizations detail page.
```

**Explicit non-goals:** No autonomous optimization — every mutation requires the explicit confirm step. No generic policy-as-code framework, exactly the five documented controls. No measurement of the outcome yet (Phase 6).

**Demo/test scenario proving the phase works:** Apply the Phase 3/4 opportunity's recommendation through the confirm dialog, then send a batch of requests through the gateway and confirm roughly the configured split lands on each model, with the change visible in Policy History.

---

## Phase 6 — Measure It

**Type:** Product milestone

**Product goal:** Close the loop between what Fluxen estimated and what actually happened.

**User outcome:** "Fluxen didn't just tell me I could save money — it told me, two weeks later, whether I actually did."

**Technical objective:** Freeze a baseline at apply time, measure the real outcome against it, and report an honest, volume-adjusted verdict — distinguishing estimated savings from realized savings everywhere.

**Dependencies:** Phase 5 complete — needs a real applied policy change to measure.

**Backend tasks:**
- `db/migrations/00008_measurements.sql`: `measurements` (Part E.1).
- `internal/measure/baseline.go`: freeze the 14-day pre-apply baseline including `baseline_cost_per_1k_micro`, computed once at apply time and never recomputed (Rule 10 — no silent semantic drift, even if the pricing catalog changes later). Wired into `apply.go`'s transaction from Phase 5.
- `internal/measure/compare.go` + `verdict.go`: cost-per-1,000-requests comparison (never raw before/after totals — volume always moves) and the five-way verdict (`successful`/`partial`/`no_effect`/`regressed`/`inconclusive`), including confound detection via `policy_history` (Part G.5).
- `internal/measure/scheduler.go`: interim (+7d) and final (+14d) checks wired into the Phase 2 job scheduler; `fluxenctl` gets a clock-fast-forward affordance for testing without a real two-week wait.
- Revert path: `internal/policy/revert.go`, reusing Phase 5's `policy/revert` endpoint, so a `regressed` verdict's one-click Revert restores the exact prior `policy_history` document and marks the opportunity `reverted`.
- `GET /api/v1/measurements*`.
- Realized-savings accounting: sum of `actual_savings_micro` from `successful`/`partial` measurements plus exact-cache `cache_saved_micro` — **never** blended with estimated/projected figures (Part G.6).

**Frontend tasks:**
- Measure section of the Optimizations detail page (Part I.2): collecting/interim/final states, before/after comparison chart, verdict display with plain-language explanation, Revert button on `regressed`.
- Every monetary figure across the app now carries its value-type badge (`estimated|measured|projected|realized`, Part G.6) — this is the phase where that convention becomes real rather than aspirational, since it's the first time all four value types exist simultaneously in one place.

**Database tasks:**
- `measurements`.

**API contracts:**
- `GET /api/v1/measurements`, `GET /api/v1/measurements/{id}`

**Tests:**
- Unit: all five verdict outcomes, confound detection, baseline stability under a simulated catalog change.
- Integration: full apply → (fast-forwarded) interim → final → verdict transaction; a deliberately-regressed scenario → Revert → confirm the policy document exactly matches pre-apply state.

**Acceptance criteria:** On the Phase 5 applied change, after simulated time passage, Fluxen reports a verdict whose cost-per-1,000-requests math is independently verifiable from the underlying `requests` rows, and a deliberately regressed scenario reverts cleanly.

**Exit criteria:**
```text
Fluxen can tell the user whether an applied optimization actually
worked — with estimated savings and realized savings clearly and
separately labeled everywhere they appear, for the complete Aha-Moment
recommendation carried end to end from Phase 3 through this phase.
```

**Explicit non-goals:** No measurement dimension beyond cost (no latency/quality verdicts — Fluxen does not evaluate quality, Rule 18). No autonomous revert — regression only ever surfaces a one-click action for the human.

**Demo/test scenario proving the phase works:** Apply the running example's recommendation, fast-forward the clock via `fluxenctl`, and see a completed measurement whose verdict and numbers are self-consistent with the seeded traffic's known behavior before and after the change — including one deliberately-seeded regression case that reverts cleanly.

---

## Phase 7 — Complete V1 Product

**Type:** Productization

**Product goal:** Deliver the remaining V1 product surface the PRD requires, now that the core loop is proven — breadth after depth, never before.

**User outcome:** "Everything the PRD promised is here: more providers, the full dashboard, and a production-ready install — on top of a core loop that already worked before any of this was added."

**Technical objective:** Fill in the parts of V1 that are real requirements but were correctly deferred because they were not required to prove the product hypothesis: the remaining two providers, the remaining three detectors, the efficiency score, the remaining dashboard screens at full spec, and production readiness.

**Dependencies:** Phase 6 complete. This phase must not start early, and none of its tasks may be pulled forward into earlier phases even where it would be technically convenient (Part L.0).

**Backend tasks:**
- `pkg/providers/gemini`: client, translation, streaming usage, safety-block handling; `pkg/providers/ollama`: client, translation, streaming usage, `/api/tags` discovery. Both wired into the gateway (`handler_gemini.go`, `handler_ollama.go`) and `provider_credentials` CRUD + encrypted storage + health checks (Part D).
- Cost normalization for Gemini (catalog-priced) and Ollama (`cost_status=local`, no GPU/electricity model — Part D.1/H.2, frozen decision, do not revisit).
- Repeated Request detector (Part G.3.2), Token Efficiency detector (Part G.3.3), Traffic Anomaly detector (Part G.3.4) — each with its own fixture in the detector-quality suite (Part K), each required to stay silent on clean traffic.
- `internal/score`: efficiency score, five weighted components, daily snapshot, "not enough data" gate (Part J of the earlier spec / PRD §13).
- Overview API (`GET /api/v1/overview`, `.../timeseries`) and the Requests investigation API (`GET /api/v1/requests`, cursor-paginated).
- Retention enforcement (`internal/store/retention.go`) if not already built incidentally.
- Production Docker images (multi-stage, distroless), finalized `docker-compose.yml` as the one documented install path (Part B.4).
- Security pass: credentials encrypted at rest verified, keys never returned by any API, bodies off by default with redaction when enabled, CORS/CSRF scoped correctly, rate limiting on the control API itself.
- Load testing against the hot-path latency/throughput targets (Part B) — release-blocking from this phase onward.

**Frontend tasks:**
- Overview page (Part I.3) in full.
- Policies matrix (`/policies`, Part I.4) — the per-app editor already exists from Phase 5; this adds the cross-app view.
- Requests investigation screen (Part I.5) in full.
- Settings screens (Providers, Pricing, Users, Retention — Part I.6) to full functional parity.
- Application Detail's Efficiency tab completed with the real score breakdown; Opportunities tab completed with all four detector kinds' evidence panels.
- Optimizations list (Part I.2) completed with full filter/sort UX if not already done in Phase 3.
- Public website (`web/apps/website`): Home, Product, How it Works, Why Fluxen, Providers, Pricing, Docs/Getting Started.
- `docs/`: self-hosting guide, quickstart, API reference (generated from `openapi.yaml`), concepts pages (efficiency score formula published, Rule 17), provider setup guides.

**Database tasks:**
- `db/migrations/00009_provider_credentials.sql`, `00010_efficiency_scores.sql` (or folded earlier if convenient at migration-authoring time — the constraint is on when features become user-visible, not migration numbering).

**API contracts:**
- `GET /api/v1/overview*`
- `GET /api/v1/requests*`, `GET /api/v1/requests/{id}`
- `POST /api/v1/providers/credentials`, `GET /api/v1/providers/{provider}/models`, `POST /api/v1/providers/{id}/health-check`
- `GET /api/v1/applications/{id}/score`, `.../score/history`
- `GET/PATCH /api/v1/settings`

**Tests:**
- Golden tests for Gemini and Ollama translation (both directions).
- The complete detector-quality suite (Part K): five labelled fixtures (clean, expensive-model, duplicate-heavy, token-growth, spiky), all four detectors, zero false positives on clean traffic across all of them — release-blocking.
- Full E2E journey (Part L.Final / Part J) scripted in Playwright against a freshly-composed stack in CI, release-blocking.
- Load test gating merges to main.
- Manual clean-installation test performed by someone who did not write the code.

**Acceptance criteria:** The Part L.Final sixteen-step Definition of Done, completed end to end by someone unfamiliar with the codebase, using only the README, in approximately 15 minutes.

**Exit criteria:**
```text
The complete V1 product surface from the PRD is delivered: Overview,
Applications, Application Detail, Requests, Optimizations, Policies,
Settings, all three providers, and production readiness — built on top
of a core loop that was already fully working and demoed before this
phase began.
```

**Explicit non-goals:** Nothing in Part A.2, ever, regardless of how natural it would feel to add while touching adjacent code. This phase makes V1 complete and production-ready; it does not expand V1.

**Demo/test scenario proving the phase works:** A person who has never seen Fluxen clones the repo, runs `docker compose up`, and — using only the README and the in-product wizard — reaches a measured, revertible optimization outcome for a real or Ollama-routed application, having also used Gemini for at least one request along the way, in about 15 minutes.

---

# Part M — Claude Execution Protocol

Work through this specification in the following loop, one task at a time, never skipping ahead — and never skipping ahead of the Aha Moment in particular. Phase 3 is not "just another phase"; it is the phase every earlier phase exists to serve and every later phase exists to build on. Do not let convenience pull a Phase 7 task (a second provider, a fourth detector, Overview, Settings) earlier just because it's adjacent to code already open.

```text
Read specification (relevant Part(s) for the current task)
      ↓
Select current phase (Part L, in order — never skip a phase)
      ↓
Select next incomplete task within that phase
      ↓
Inspect existing repository state (don't assume — check what's actually there)
      ↓
Implement the task
      ↓
Run the relevant test suite
      ↓
Fix failures
      ↓
Review the task's own "done when" statement, and the phase's acceptance criteria
      ↓
Mark task complete
      ↓
Move to the next task (or, if the phase is complete, verify exit criteria before
      starting the next phase)
```

**At the start of every phase**, report:

```text
Current phase:
Type (Foundation / Product milestone / Supporting capability / Productization):
Goal:
Tasks remaining:
Relevant specification sections:
Dependencies (what must already exist):
```

**After every task**, report:

```text
Implemented:
Files changed:
Tests added/updated:
Tests executed:
Result:
Remaining work (in this phase):
```

Do not jump to a future phase until the current phase's exit criteria (Part L) are demonstrably satisfied — not merely "the code looks right," but the stated exit-criteria check actually run and passing. If a task's specification is ambiguous in a way that affects architecture or product behavior, stop and ask rather than guessing (Rules 2–3, Part F).

**One additional rule specific to this plan's sequencing:** if implementing a phase surfaces a task that would clearly belong to a later phase (e.g. while building the Phase 3 detector, a second detector kind seems easy to add too), do not implement it. Note it, and let the phase it belongs to pick it up. The value of this plan comes from each phase staying provably, narrowly complete — not from doing more work sooner.

---

# Part L (final) — V1 Definition of Done

The ultimate acceptance test for the entire build, and — read as a sequence — the same order Part L's phases deliver it in. A new user, using only the README and in-product UI, must be able to:

1. Run Fluxen (`docker compose up`). — *Phase 0*
2. Complete setup (create the owner account). — *Phase 1*
3. Create an application. — *Phase 1*
4. Create an API key. — *Phase 1*
5. Configure a provider (OpenAI at minimum; Gemini or Ollama once Phase 7 ships). — *Phase 1 / 7*
6. Change their application's `base_url` to point at Fluxen. — *Phase 1*
7. Send real traffic. — *Phase 1*
8. See attributed requests appear in the dashboard. — *Phase 1*
9. See usage and cost for those requests. — *Phase 2*
10. See application-level analytics (Application Detail). — *Phase 2*
11. Receive a meaningful optimization opportunity — the Aha Moment — or, on light traffic, correctly see "No meaningful optimization found" / "Not enough data yet" rather than noise. — *Phase 3* ⭐
12. Understand the evidence behind that opportunity. — *Phase 3* ⭐
13. Simulate it. — *Phase 4*
14. Apply it with explicit confirmation. — *Phase 5*
15. Measure the result. — *Phase 6*
16. Revert a regression, if one occurs. — *Phase 6*

The product must demonstrate the complete **Understand → Identify → Simulate → Control → Measure** loop, end to end, in roughly 15 minutes on the user's own infrastructure. Steps 1–10 are table stakes that competitors already meet. **Steps 11–12 are the product** — everything before them exists to make them possible on real data, and everything after them (13–16) exists to make them trustworthy enough to act on.
