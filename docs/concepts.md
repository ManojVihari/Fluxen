# Concepts

## Applications

The fundamental unit of optimization in Fluxen. Every proxied request is attributed to exactly one application via its API key. Applications have their own usage, cost, efficiency score, opportunities, policy, and request history.

## Requests

The raw fact table — one row per proxied request, attributed, priced, and token-counted. Nothing in the dashboard queries this table directly except the Requests investigation screen; every other view reads a pre-computed rollup for speed.

## Cost and value labeling

Every dollar figure Fluxen shows is labeled with what kind of number it is:

- **estimated** — a detector's own calculation from real historical traffic, not yet simulated or applied.
- **projected** — a shorter window's total, scaled to a 30-day month for comparability.
- **measured** — a real, observed number from actual traffic (a simulation's replay, or a measurement's baseline/observed window).
- **realized** — the actual savings a measurement's final verdict confirmed — the only figure with `status: final`.

Cost is always integer micro-USD (1 USD = 1,000,000 micro-USD), computed once and never recomputed from a rounded display value. A model with no catalog entry gets `cost_status: unknown` and `cost_micro: 0` — never a fabricated `$0.00`. Ollama traffic gets `cost_status: local` — Fluxen has no GPU/electricity cost model, and local traffic is visually and numerically excluded from billed-cost totals unless a view explicitly labels it "local."

## Opportunities

Detector output — the product's primary noun. Every opportunity has a lifecycle: `open → reviewed → simulated → applied → measured`, or `dismissed`/`stale` at any point before applying. Four kinds:

- **model_cost** — traffic on an expensive model that a cheaper, catalog-declared alternative could plausibly handle.
- **repeated_request** — exact-duplicate request traffic a TTL cache would have served for free.
- **token_efficiency** — sustained drift in input or output tokens per request.
- **traffic_anomaly** — statistically unusual volume, cost, tokens, or error rate against the application's own seasonal baseline. This kind carries no savings figure — it's a stability signal, not a cost claim.

An opportunity only appears once an application clears a minimum traffic/spend floor (roughly 1,000 requests and $5 of spend over the trailing 14 days) and the finding itself clears a minimum savings floor (roughly $10/month and 5%) — below either, Fluxen says "not enough data," never guesses.

## The Efficiency Score

A 0–100 score per application, computed daily, with five weighted components: model efficiency, token efficiency, cache efficiency, traffic stability, and cost efficiency. Each component is derived from the same detector logic that powers the Opportunities tab, so the score's own reasons are always traceable to evidence you can inspect. An application below the same traffic/spend floor opportunities use gets `status: insufficient_data` instead of a score.

## Policies

Five controls, each independently enabled or disabled, per application: model routing (percentage-based), exact caching, budget, rate limit, model restriction. Every change is versioned with a diff and an optional note in `policy_history`. Applying an opportunity's recommendation and hand-editing the policy in Settings both write to this same document.

## Simulation

A pure replay of a proposed scenario (model mix or exact caching) against an application's real historical requests, using the exact same pricing/caching logic production uses. Never changes production traffic — it's how a recommendation gets proven before it's applied.

## Measurement

The before/after outcome of an applied change: a frozen pre-apply baseline, an interim (+7d) and final (+14d) check comparing cost per 1,000 requests (not raw totals, since traffic volume always moves), and a five-way honest verdict — `successful`, `partial`, `no_effect`, `regressed`, or `inconclusive`. A `regressed` verdict always surfaces a one-click Revert that restores the exact prior policy.
