# Fluxen

**AI Traffic, Optimized.**

Fluxen is a self-hosted AI traffic gateway and optimization platform. See:

- [`Fluxen V1 — Product Requirements Document.md`](./Fluxen%20V1%20—%20Product%20Requirements%20Document.md) — what Fluxen is and why.
- [`Fluxen V1 — Implementation Plan.md`](./Fluxen%20V1%20—%20Implementation%20Plan.md) — the developer-ready implementation specification and phased build plan. This is the primary engineering reference; read it before changing code.
- [`docs/`](./docs/README.md) — quickstart, self-hosting guide, concepts, provider setup, and API reference for using an already-built Fluxen deployment (as opposed to the two documents above, which are for building it).

## Current status

**Phase 0** through **Phase 6** are implemented (see git history / earlier README revisions for their individual "what's new" notes). **Phase 7 — Complete V1 Product** is substantially implemented:

- The three remaining detectors (repeated request, token efficiency, traffic anomaly) alongside Model Cost, all four wired into one `detect.run` job.
- The Efficiency Score (`internal/score`): five weighted components, computed daily, reusing the same detector logic the Opportunities tab shows.
- Gemini and Ollama providers, fully translated in both directions (including streaming), behind the same single OpenAI-compatible gateway endpoint as OpenAI — routing resolved per-model against the pricing catalog, with Ollama as the catch-all for uncataloged models when configured.
- Org-managed, AES-256-GCM-encrypted provider credentials (Settings → Providers) with health checks — the real replacement for the Phase 1-6 env-var credentials, which still work as a fallback.
- The Overview page (org-wide entry point), the Requests investigation screen (cursor-paginated, with a detail drawer), and the Policies matrix (cross-app view) — all backed by new read APIs.
- Settings screens: Providers, Pricing (read-only catalog view), Users (list + invite via a one-time generated password — V1 has no email infrastructure), and Retention (the three retention knobs, enforced daily by a new `retention.enforce` job).
- The public marketing website (`web/apps/website`): Home, Product, How it Works, Why Fluxen, Providers, Pricing, Docs.

Known gaps, flagged rather than silently skipped: per-org editable pricing overrides (named in the PRD but never specified anywhere — no data model, no rule for how it interacts with pricing-history immutability); a generated OpenAPI spec (the API reference in `docs/` is hand-maintained instead); production Docker hardening, a dedicated security pass, load testing, and a scripted E2E suite are still open (next up).

- Repository structure, local dev loop, Postgres + Redis connectivity with migrations, structured logging, health/readiness/metrics endpoints.
- A control API (`/api/v1/...`) for first-run setup, login/logout, applications, and API keys.
- An OpenAI-compatible gateway (`/v1/chat/completions`) that authenticates by API key, proxies to real OpenAI (streaming and non-streaming), and persists every request — attributed, priced, and token-counted — to Postgres.
- An in-process background scheduler that rolls raw requests up into hourly/daily aggregates, and a control API surface (`summary`/`timeseries`/`models`) that reads them.
- The **Model Cost detector**: finds requests served by an expensive model that a cheaper cataloged model could plausibly have handled, and turns that into a real, evidence-backed optimization opportunity — Fluxen's core "aha moment."
- A **simulation engine**: replays real historical traffic through a model-mix, exact-caching, or budget-impact scenario, using the exact same pricing function production uses, so a user can prove an opportunity's impact against their own history before anything changes.
- A real **control surface**: model routing, exact caching, budget, rate limiting, and model restriction — all five now actually enforced on the live gateway, versioned and diffed in policy history, and applicable straight from a proven recommendation.
- **Measurement**: every applied change gets a real before/after verdict — a frozen pre-apply baseline, an interim (+7d) and final (+14d) check comparing cost per 1,000 requests, a five-way honest verdict, and a one-click Revert on a regression that restores the exact prior policy.
- A dashboard: setup wizard, login, applications list, an Application Detail view (Usage & Cost, Models, Efficiency, and **Policies** tabs), a connect screen, and an Optimizations list + detail screen where Simulate, Apply, and now **Measure** are all fully wired end to end.
- `fluxenctl seed --demo` — seeds a demo organization, a `document-ai` application, ~30 days of realistic synthetic traffic, and immediately runs the Model Cost detector against it, so a fresh install shows a real opportunity — and a runnable simulation — without waiting.
- `fluxenctl measure check --fast-forward=<duration>` — runs interim/final measurement checks as if that much time had passed since apply, so a demo or test doesn't need to wait two real weeks.

(All of the above is Phase 0-6; see "Current status" above for what Phase 7 added on top of it.)

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

### What's new in Phase 3

Phase 3's goal — the highest-priority product milestone in the spec — is the Aha Moment: Fluxen looks at an application's own real traffic and finds a credible, explainable optimization. This phase added:

| Area | What it does |
|---|---|
| `pkg/pricing` catalog fields | Every catalog entry now also declares `context_window`, `max_output`, `capabilities`, `tier`, and `downgrade_candidates_for` — the *only* source of candidate models a detector may propose (never invented at detection time). |
| `internal/detect` | The `Detector` shape, Part G.2's suppression/ranking rules (minimum traffic/spend, minimum savings, ranked by savings × confidence, capped at 5 open opportunities per app), and the **Model Cost detector** itself: eligibility predicate, per-request recosting against the real candidate price, exclusion-reason breakdown, confidence tiering, and the conservative `min(eligible_fraction, 0.5)` recommendation split. Pure and unit-testable — no database in the detection logic itself. |
| `internal/detect.Runner` | The only piece that touches Postgres: walks every application, applies the app-level floor, fetches facts, runs the detector, and upserts survivors — re-running against unchanged traffic updates the existing opportunity instead of duplicating it, and never resets a user's review. Runs as a new scheduled job, `detect.model_cost`, every 15 minutes. |
| `db/migrations/00005` | The `opportunities` table (Part E.1) — the product's primary noun — with a partial unique index so a detector re-run can never create a duplicate "live" opportunity. |
| `GET .../opportunities`, `GET .../opportunities/{id}`, `POST .../opportunities/{id}/review` | Three new control-API endpoints. Evidence and recommendation are served as real detector JSON, not a fixed schema — every monetary figure carries `value_type: "projected"` (Rule 17: never present an estimate as measured). |
| Optimizations screens (dashboard) | A minimal `/optimizations` list and a full `/optimizations/{id}` detail page — Why, Evidence (eligibility bar, exclusion breakdown, token percentiles, price ratio, confidence badge with tooltip, the fixed no-quality-claim caveat), and Impact sections, all rendered from real evidence. Simulate/Apply buttons are present but visibly disabled — a preview of Phase 4/5, not yet wired. |
| Application Detail's Efficiency tab | No longer a disabled placeholder — shows the application's own open opportunities (at minimum a count), linking into the Optimizations detail page. The full efficiency score/ring is still Phase 7. |
| `tools/trafficgen` + `fluxenctl seed --demo` | Tuned traffic volume so a fresh seed's trailing 14 days reliably clears the detector's floors, and now runs the Model Cost detector immediately after seeding — a fresh install shows a real opportunity without waiting for the next scheduled tick. |

### What's new in Phase 4

Phase 4's goal is: let the user prove an opportunity's impact using their own history, without touching production. This phase added:

| Area | What it does |
|---|---|
| `pkg/types.CacheKey` | The exact-match cache key formula (Part G.3.2), computed and stored on every new request from this phase onward (`requests.cache_key`) — a prerequisite the specification assumed already existed; it didn't (Phase 1 never wired it in), so this phase adds it. Historical requests from before this change have no cache key and can never register a cache hit in a simulation — only new traffic does. |
| `internal/sim` | The simulation engine (Part G.4): a bounded-memory chronological replay (2M-row cap, uniform sampling beyond that) plus two pure, unit-tested scenarios — **model mix** (reroute a share of one model's traffic to a cheaper candidate, recosting every request via the exact same `pkg/pricing.Calculate` the gateway uses) and **exact caching** (a true replay through a simulated TTL+size-capped LRU, not a formula estimate). Budget-impact is deferred to Phase 5 alongside the budget control it simulates. |
| **Release-blocking self-check** | Replaying real historical requests through `pkg/pricing.Calculate` must reproduce their actually-recorded cost within 0.5% (Part G.4) — the credibility floor for the whole feature, run against real Postgres data in CI from this phase onward. There is no policy engine yet (Phase 5), so "replaying the currently applied policy" reduces to confirming pricing itself hasn't silently drifted; the check extends to real routing decisions once Phase 5 ships one. |
| `db/migrations/00006` | The `simulations` table (Part E.1) — append-only, one row per run. |
| `POST /api/v1/simulations`, `GET /api/v1/simulations/{id}`, `GET /api/v1/applications/{id}/simulations` | Runs synchronously (replay at V1 scale completes well within a request) and persists the result. `actual_cost_micro`/`replayed_requests` are measured straight from real rows; `simulated_cost_micro`/`delta_micro`/`projected_monthly_savings_micro` are `value_type: "estimated"` (Rule 17). |
| Optimizations detail page's Simulate section | Now fully functional for model-cost opportunities: a traffic-weight slider pre-filled from the opportunity's own recommendation, a real "Run simulation" call, and a result view — current vs. simulated cost, delta, affected-request count, per-model breakdown, and the fixed ±15% token-count assumption. Apply stays visibly disabled (Phase 5). |

### What's new in Phase 5

Phase 5's goal is: let the user safely act on the Aha Moment's recommendation, and have Fluxen actually enforce it. This phase added:

| Area | What it does |
|---|---|
| `pkg/policy` | The `PolicyDocument` schema for all five controls (model routing, exact caching, budget, rate limit, model restriction) and the pure `Evaluate`/`EvaluateBudget` functions — the same functions internal/sim's budget scenario and the live gateway both call (Rule 9), never duplicated between them. |
| `internal/policy` | The Postgres-backed store (optimistic-concurrency versioned saves, append-only history, diff-before-save), an in-process snapshot cache (5s TTL + Redis pub/sub invalidation for a future split deployment), and the `Applier` — the one apply transaction: validate, save with a linked opportunity/simulation history entry, transition the opportunity to `applied`, invalidate the cache. |
| `internal/guard` | Rate limiting (a Redis fixed-window counter) and budget enforcement (a Redis period-spend counter), both **fail-open** if Redis is unreachable — a control's own infra hiccup must never become an outage for real traffic. |
| `internal/cache` | Exact-match response caching, live: a Redis-backed store keyed by the cache key Phase 4 introduced, with byte-for-byte SSE replay for cached streaming responses. |
| Live gateway pipeline | All five controls now actually run on every request: model restriction and routing are pure decisions (`pkg/policy.Evaluate`); rate limit and budget are checked before calling the provider; a cache hit skips the provider call entirely. An app with no policy still behaves exactly like Phase 1 — nothing is enforced unless explicitly configured. |
| `internal/sim`'s budget scenario | The third and final Part G.4 scenario, deferred from Phase 4 to build alongside the control it simulates: replays chronologically and reports what traffic **would have been rejected** — framed as impact, never as savings (a budget is a control, not an optimization). |
| `db/migrations/00007` | `policies` (current document per app) and `policy_history` (every mutation, append-only, linked to the opportunity/simulation that caused it when applicable). |
| `POST /opportunities/{id}/apply`, `GET/PUT .../policy`, `GET .../policy/history`, `POST .../policy/revert` | The real Apply transaction and the direct policy editor's CRUD, both funneling through the same versioned save path. |
| Policies tab + Apply confirmation dialog (dashboard) | The Policies tab is no longer a placeholder — a real five-control editor with a diff-before-save and history. The Optimizations detail page's Apply button now opens a real confirm dialog requiring the application's slug to be retyped (Rule 19: friction on purpose for an irreversible-feeling change) before calling the real endpoint. |

### What's new in Phase 6

Phase 6's goal is: close the loop between what Fluxen estimated and what actually happened. This phase added:

| Area | What it does |
|---|---|
| `internal/measure` | Cost-per-1,000-requests comparison and Part G.5's exact five-way verdict (`successful`/`partial`/`no_effect`/`regressed`/`inconclusive`) — pure, unit-tested math — plus a `Runner` that finds every measurement awaiting its interim (+7d) or final (+14d) check, computes the real observed window from actual requests, detects a confounding second policy change via `policy_history`, and persists the verdict. |
| `internal/policy`'s `Applier`/`Reverter` | Apply now freezes a real 14-day pre-apply baseline (`baseline_cost_per_1k_micro`, computed once and never recomputed — Rule 10) via a small injected interface (`BaselineFreezer`) that avoids a package import cycle. A `regressed` verdict's one-click Revert restores the exact prior policy document as a new version and marks both the opportunity and the measurement `reverted`. |
| `db/migrations/00008` | `measurements` — one row per apply, baseline frozen at creation, observed/verdict fields filled in as checks complete. |
| `GET /measurements`, `GET /measurements/{id}`, `GET /opportunities/{id}/measurement`, `POST /measurements/{id}/revert` | The measurement read surface and the Revert action. `actual_savings_micro`/`actual_pct` are `null` until `status` is `final` — Part G.6's fourth value type, "realized," only exists once a verdict is actually in. |
| `fluxenctl measure check --fast-forward=<duration>` | Runs interim/final checks as of "now + duration" instead of real wall time — the demo/testing affordance the spec calls for so nobody has to wait two real weeks to see a verdict. |
| Measure section (dashboard) | Appears on the Optimizations detail page once an opportunity has been applied: collecting/interim/final states, the baseline-vs-observed comparison, a plain-language verdict explanation, and — only on `regressed` — a Revert confirmation dialog. |

## Quickstart (Docker Compose)

Requires Docker and Docker Compose. No `.env` file, no exported variables, no manual key generation — every setting below has a working default.

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

This creates an organization (owner: `owner@example.com` / `supersecret123`, only if no organization exists yet), an application named "Document AI", ~30 days of synthetic multi-model traffic, and runs the Model Cost detector against it — then sign in, open **Optimizations** (or the application's **Efficiency** tab), open the opportunity, and click **Run simulation** to see the model-mix recommendation's impact against that same real traffic. Safe to run more than once; it no-ops if the application already has traffic.

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

For the gateway to actually reach a provider, add a credential from **Settings → Providers** in the dashboard (OpenAI, Gemini, or a self-hosted Ollama endpoint) — it's encrypted at rest with a key Fluxen generates for itself on first boot, no `FLUXEN_ENCRYPTION_KEY` required. Without a credential, the gateway still runs, but every chat request returns `503 no_provider_credential` until one exists. (An `OPENAI_API_KEY` env var works too, as a deployment-wide alternative — see [`docs/self-hosting.md`](docs/self-hosting.md).)

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
| `internal/detect` | The eligibility predicate, confidence tiering, and conservative-split math are each correct against hand-computed fixtures. **Release-blocking:** an "expensive-model" fixture produces exactly one opportunity with internally-consistent numbers; a "clean" fixture (already on the cheap model, or genuinely needing the premium one) produces **zero**. End to end against real Postgres: the full detect → open → reviewed lifecycle works, an application below the traffic/spend floor produces nothing, and re-running the detector against unchanged traffic never creates a duplicate. |
| `internal/sim` | Model-mix recosting and the conservative-split affected-request count are exact against hand-computed fixtures; exact-caching correctly hits within a TTL, misses once it expires (including the spec's own "six days later" example), never hits across different keys or with no key at all, and respects a size-capped LRU; the budget scenario blocks only once a period's spend reaches its limit, never lets a blocked request's cost count, and resets cleanly across period boundaries. **Release-blocking:** replaying real Postgres-stored requests through `pkg/pricing.Calculate` reproduces their recorded cost exactly — the 0.5% self-check floor (Part G.4). |
| `pkg/policy` | Model restriction blocks exactly the disallowed model and nothing else; percentage routing converges to the configured weight over 100k trials and sticky routing always picks the same variant for the same key; every document-validation rule (bad weight, unknown period/mode, empty allow-list, ...) is rejected. |
| `internal/guard` | Rate limiting allows exactly up to the configured count and blocks the next request; budget blocks once (and only once) spend reaches the limit, soft mode never blocks, periods are isolated from each other; **both fail open** when Redis is unreachable. |
| `internal/cache` | Set/Get round-trips exactly, entries expire on their TTL, different apps' entries never collide, an empty cache key never hits, streamed chunks round-trip in order. |
| `internal/policy` | Save is version-checked (a stale `expected_version` is rejected, never silently overwritten); history records every mutation in order; `Diff` reports only the fields that actually changed. **`Applier`**: the full apply transaction end to end (policy saved, opportunity transitioned to `applied`, history linked to both the opportunity and the simulation) and rejects applying without a matching simulation. |
| `internal/gateway` (Phase 5 additions) | Against real Postgres + Redis: model restriction returns 403 for a disallowed model and 200 for an allowed one; rate limiting returns 429 exactly once the configured limit is exceeded; budget returns 403 once the limit is reached; **a live routing split converges to its configured weight over 300 real requests through the full HTTP pipeline**; a second identical cached request hits and the fake provider is called exactly once; an application with no policy at all behaves exactly like Phase 1. |
| `tools/trafficgen` | Generated traffic only uses cataloged (priced) models; the injected model-cost inefficiency is actually present in the output; generation is deterministic for a fixed seed; `Seed()` is idempotent end-to-end against real Postgres. |
| `internal/measure` | Every one of Part G.5's five verdicts against hand-computed fixtures, including the precedence resolution for bands that can overlap (a regression is never reclassified as "successful" just because the original estimate was tiny) and the low-volume/confound inconclusive gates. **End to end against real Postgres:** apply → seed cheaper post-apply traffic → fast-forward through interim and final → a `successful` verdict whose numbers match hand computation; a separate deliberately-regressed scenario → Revert → the policy document exactly matches pre-apply state; a second policy change landing mid-window → `inconclusive`. |

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
6. **Confirm the opportunity itself, and that re-detection doesn't duplicate it:**
   ```bash
   curl -s -b cookies.txt "http://localhost:8080/api/v1/opportunities" | python3 -m json.tool
   docker compose restart fluxen   # detect.model_cost also runs once immediately at boot
   docker compose exec postgres psql -U fluxen -d fluxen -c "SELECT count(*), status FROM opportunities GROUP BY status;"
   ```
   Expect exactly one `model_cost` opportunity, with a caveat string, an exclusion breakdown, and `savings_pct`/`savings_micro` that clear Part G.2's floors — and the restart must not create a second row or reset a `reviewed` opportunity back to `open`.
7. **Open the dashboard**, click **Optimizations** (or an application's **Efficiency** tab), and open the opportunity: Why/Evidence/Impact should match step 6's numbers.
8. **Run the pre-filled simulation** and cross-check it against the opportunity's own numbers:
   ```bash
   curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/simulations \
     -H "Content-Type: application/json" \
     -d "{\"app_id\":\"$APP_ID\",\"opportunity_id\":\"$OPPORTUNITY_ID\",\"scenario\":{\"type\":\"model_mix\",\"current_model\":\"gpt-4o\",\"candidate_model\":\"gpt-4o-mini\",\"traffic_weight\":0.5}}" \
     | python3 -m json.tool
   ```
   `actual_cost_micro × 30 ÷ 14` must equal the opportunity's own `current_cost_micro` exactly (both are the same real requests, over the same window, priced through the same function) — if that ever drifts, something in `internal/sim`/`internal/detect` has diverged from Rule 9. In the dashboard, the same "Run simulation" button on the opportunity page should show matching numbers.
9. **Confirm the release-blocking self-check still passes**: `go test ./internal/sim/... -run TestSelfCheck -v` — a failure here means replay no longer reproduces recorded reality and blocks the release (Part G.4/K).
10. **Apply the recommendation and watch it actually enforce.** In the dashboard, on the opportunity page, click **Apply**, type the application's slug to confirm, and submit — or via curl:
    ```bash
    curl -s -b cookies.txt -X POST "http://localhost:8080/api/v1/opportunities/$OPPORTUNITY_ID/apply" \
      -H "Content-Type: application/json" \
      -d "{\"confirm\":true,\"simulation_id\":\"$SIMULATION_ID\",\"routing\":{\"from_model\":\"gpt-4o\",\"to_model\":\"gpt-4o-mini\",\"weight\":0.5,\"sticky\":true}}" \
      | python3 -m json.tool
    ```
    Then send one real request for the `from_model` (see "Connect a real application") and confirm the JSON response's own `"model"` field comes back as the `to_model` — the policy is genuinely rerouting live traffic, not just recording an intent. `docker compose exec postgres psql ... "SELECT route_reason, route_variant, policy_version FROM requests ORDER BY started_at DESC LIMIT 1;"` should show `split`, `B`, and the new version number. The opportunity's own status should now read `applied`.
11. **Check the Policies tab and history.** Open the application's **Policies** tab in the dashboard — it should show the routing control now enabled with the applied weight, and the history section underneath should list the apply as one entry sourced from `opportunity`, linked to both the opportunity and the simulation. Try editing a different control (e.g. enabling the rate limit) directly in the editor and saving — it should appear as a separate `user`-sourced history entry, and the routing control from the apply should be untouched.
12. **Restart the stack** once more and confirm the applied policy is still enforced (a container restart must not lose or need to re-derive it — `internal/policy.Snapshot` reloads from Postgres on its next read).
13. **Confirm the baseline froze at apply time:**
    ```bash
    curl -s -b cookies.txt "http://localhost:8080/api/v1/opportunities/$OPPORTUNITY_ID/measurement" | python3 -m json.tool
    ```
    `status` should be `collecting`, with `baseline_requests`/`baseline_cost_per_1k_micro` already populated and `observed_*`/`verdict` still absent. `baseline_cost_per_1k_micro` should equal (by hand) `sum(cost_micro)/count(*) * 1000` over `requests` in `[baseline_start, baseline_end)` for this app — verifiable directly against the `requests` table.
14. **Fast-forward through both checks and see a real verdict**, without waiting two weeks:
    ```bash
    docker compose exec fluxen fluxenctl measure check --fast-forward=14d
    curl -s -b cookies.txt "http://localhost:8080/api/v1/opportunities/$OPPORTUNITY_ID/measurement" | python3 -m json.tool
    ```
    `status` should now be `final` with a `verdict`. If no real traffic landed in the observed window (`[applied_at, applied_at+14d)` — note this is real wall-clock time, fast-forwarding only moves the *check*, not traffic), expect `inconclusive` with the low-volume reason, which is itself the correct, honest behavior (Part G.5) — send enough real requests for the `from_model` (see step 10) to clear 30% of `baseline_requests`, then re-run the check, to see a `successful` verdict instead. In the dashboard, the opportunity page's **Measure** section should show the same numbers.
15. **Deliberately trigger and revert a regression.** With a `final` measurement in hand (of any verdict — the endpoint itself doesn't require `regressed`, though the dashboard button only appears then), confirm the current policy first, then:
    ```bash
    curl -s -b cookies.txt "http://localhost:8080/api/v1/applications/$APP_ID/policy" | python3 -m json.tool
    curl -s -b cookies.txt -X POST "http://localhost:8080/api/v1/measurements/$MEASUREMENT_ID/revert" \
      -H "Content-Type: application/json" -d '{"confirm":true,"note":"testing revert"}' | python3 -m json.tool
    ```
    The returned document must exactly match what the policy was *before* the apply (empty, if this was the application's first-ever policy change) — check `policy_history` to confirm it landed as a brand-new version with `change_source="revert"`, never overwriting the earlier entries. The opportunity and the measurement should both now read `status: "reverted"`.

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
| `OPENAI_API_KEY` | no | A deployment-wide OpenAI credential, as an alternative to adding one from Settings → Providers in the dashboard. Without either, `/v1/chat/completions` returns `503 no_provider_credential`. |
| `FLUXEN_ENCRYPTION_KEY` | no | Overrides the encryption key Fluxen otherwise generates and persists itself on first boot (see `FLUXEN_DATA_DIR`) for Settings → Providers' stored credentials. |
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
GET      /api/v1/opportunities?status=&app_id=     detector output, newest-detected first
GET      /api/v1/opportunities/{id}                one opportunity's full evidence/recommendation
POST     /api/v1/opportunities/{id}/review         open -> reviewed
POST     /api/v1/opportunities/{id}/apply          apply a proven recommendation as a new, enforced policy version
POST     /api/v1/simulations                       run a model_mix, exact_caching, or budget scenario, synchronously
GET      /api/v1/simulations/{id}                  one simulation's full result
GET      /api/v1/applications/{id}/simulations     an application's simulation history, newest first
GET      /api/v1/applications/{id}/policy          current policy document + version
PUT      /api/v1/applications/{id}/policy          save a new version (optimistic-concurrency checked)
GET      /api/v1/applications/{id}/policy/history  every mutation, newest first
POST     /api/v1/applications/{id}/policy/revert   restore a prior version as a brand-new one
GET      /api/v1/measurements?app_id=              an application's measurement history, newest first
GET      /api/v1/measurements/{id}                 one measurement's full baseline/observed/verdict
GET      /api/v1/opportunities/{id}/measurement    the live measurement for one opportunity, if any
POST     /api/v1/measurements/{id}/revert          undo a regression: restore the exact pre-apply policy
```

`range` accepts `24h`, `7d`, `30d` (default), `90d`.

**Gateway** (API-key auth via `Authorization: Bearer fx_live_...`):

```text
POST /v1/chat/completions          OpenAI-compatible, streaming and non-streaming
                                    (as of Phase 5: policy-enforced — may reroute, cache,
                                    rate-limit, or budget-block per the application's policy)
```

## Repository layout

```text
cmd/fluxen           combined gateway + control binary (the default deployment target)
cmd/fluxenctl        admin CLI (migrations, demo seeding)
internal/config      env config, validated at boot
internal/auth        password hashing, API keys, session store, key resolver
internal/store       Postgres access: applications, api_keys, users, organizations, requests, rollups, opportunities, simulations, measurements
internal/gateway     the OpenAI-compatible ingress: auth, policy, routing, cache, guard, pipeline, streaming
internal/ingest      async bounded queue + batch writer from gateway to Postgres
internal/rollup      idempotent hourly/daily aggregation of requests into the rollup tables
internal/detect       all four detectors (model cost, repeated request, token efficiency, traffic anomaly), suppression/ranking rules, and the Postgres-touching Runner
internal/sim          the simulation engine: replay, model-mix/exact-caching/budget scenarios, the release-blocking self-check
internal/score        the Efficiency Score: five weighted components reusing the detectors' own logic, daily snapshot Runner
internal/policy       the policy store, versioned history, in-process snapshot cache, and the Apply/Revert transactions
internal/measure     baseline freeze, cost-per-1k comparison, the five-way verdict, and the interim/final check Runner
internal/guard       live rate-limit and budget enforcement (Redis-backed, fail-open)
internal/cache       live exact-match response caching (Redis-backed, SSE replay for streamed hits)
internal/credentials encrypt-on-write, decrypt-only-internally provider credential service + in-process snapshot cache (mirrors internal/policy's)
internal/crypto      AES-256-GCM encryption for credentials at rest
internal/retention   daily enforcement of the requests/body/opportunity retention windows
internal/worker      the in-process background job scheduler
internal/api         the control-plane HTTP API the dashboard talks to
internal/health      dependency health checks (/readyz)
internal/observability  structured logging, Prometheus metrics
pkg/types            provider-neutral request/response/usage types, exact-match cache key
pkg/providers/openai OpenAI adapter (translation, streaming, errors)
pkg/providers/gemini  Gemini adapter (full bidirectional translation, streaming, safety-block handling)
pkg/providers/ollama  Ollama adapter (native /api/chat + /api/tags, NDJSON streaming translation)
pkg/pricing          embedded pricing catalog + cost calculation
pkg/policy           the PolicyDocument schema + pure Evaluate/EvaluateBudget (no I/O — shared by gateway and simulation)
tools/trafficgen     synthetic multi-day, multi-model traffic generator (used by `fluxenctl seed`)
db/migrations/       goose SQL migrations, embedded into the fluxen binary
docs/                self-hosting guide, quickstart, concepts, provider setup, hand-maintained API reference
web/apps/dashboard   the authenticated product (Next.js): Overview, Applications, Application Detail, Optimizations, Policies matrix, Requests, Settings
web/apps/website     the public marketing site (Next.js, statically exported): Home, Product, How it Works, Why Fluxen, Providers, Pricing, Docs
deploy/              Dockerfiles used by docker-compose.yml
```

See Part C.1 of the implementation specification for the full target module layout, and Part L for what each phase adds.
