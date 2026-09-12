package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

// Requests is the write path for the requests fact table. The only write
// method is a batch insert — Phase 1's ingest writer is the sole caller,
// and it always writes in batches (Part B.2: never a synchronous
// per-request write on the gateway's hot path).
type Requests struct {
	pool *pgxpool.Pool
}

func NewRequests(pool *pgxpool.Pool) *Requests {
	return &Requests{pool: pool}
}

const insertRequestSQL = `
	INSERT INTO requests (
		id, org_id, app_id, api_key_id, started_at, duration_ms, ttft_ms,
		endpoint, protocol, streamed,
		requested_model, provider, model, route_reason, route_variant, policy_version,
		input_tokens, output_tokens, cached_input_tokens, total_tokens, usage_source,
		cost_micro, cost_input_micro, cost_output_micro, cost_status, pricing_version,
		cache_status, cache_key, cache_saved_micro,
		status, http_status, error_code, error_message,
		has_tools, has_tool_calls, has_images, json_mode, temperature, max_tokens,
		message_count, system_prompt_hash, request_body, response_body
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7,
		$8, $9, $10,
		$11, $12, $13, $14, $15, $16,
		$17, $18, $19, $20, $21,
		$22, $23, $24, $25, $26,
		$27, $28, $29,
		$30, $31, $32, $33,
		$34, $35, $36, $37, $38, $39,
		$40, $41, $42, $43
	)
`

// InsertBatch writes a batch of usage records in one round trip via pgx's
// pipelined Batch API. A failure partway through does not roll back
// earlier records in the same batch — pgx.Batch executes each statement
// independently — so one malformed record can't silently drop the rest of
// a batch of otherwise-good ones.
func (r *Requests) InsertBatch(ctx context.Context, records []types.UsageRecord) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, rec := range records {
		batch.Queue(insertRequestSQL,
			rec.ID, rec.OrgID, rec.AppID, nullableAPIKeyID(rec.APIKeyID), rec.StartedAt, rec.DurationMS, rec.TTFTMS,
			rec.Endpoint, rec.Protocol, rec.Streamed,
			rec.RequestedModel, rec.Provider, rec.Model, rec.RouteReason, nullableString(rec.RouteVariant), rec.PolicyVersion,
			rec.InputTokens, rec.OutputTokens, rec.CachedInputTokens, rec.TotalTokens, rec.UsageSource,
			int64(rec.Cost), int64(rec.CostInput), int64(rec.CostOutput), string(rec.CostStatus), rec.PricingVersion,
			rec.CacheStatus, nullableBytes(rec.CacheKey), int64(rec.CacheSavedCost),
			rec.Status, nullableInt(rec.HTTPStatus), nullableString(rec.ErrorCode), nullableString(rec.ErrorMessage),
			rec.HasTools, rec.HasToolCalls, rec.HasImages, rec.JSONMode, rec.Temperature, rec.MaxTokensReq,
			rec.MessageCount, nullableBytes(rec.SystemPromptHash), nullableBytes(rec.RequestBody), nullableBytes(rec.ResponseBody),
		)
	}

	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := range records {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("store: failed to insert request %d of %d (id=%s): %w", i+1, len(records), records[i].ID, err)
		}
	}
	return nil
}

// ModelCostFact is the minimal per-request shape the Model Cost detector
// (internal/detect) reads — deliberately not the full requests row, so
// the detector's own pure logic can be unit-tested against a hand-built
// fixture without a database (Rule 8).
type ModelCostFact struct {
	RequestedModel string
	Provider       string
	InputTokens    int
	OutputTokens   int
	HasTools       bool
	HasToolCalls   bool
	HasImages      bool
	JSONMode       bool
	CostMicro      int64
	CostStatus     types.CostStatus
}

// ModelCostFacts returns every successfully-served request for an
// application in [since, until) — the trailing-14-day window Part G.3.1
// detects against. Only status='ok' requests are considered: an errored
// request never completed, so its token/cost figures don't represent
// real served traffic the detector should reason about.
func (r *Requests) ModelCostFacts(ctx context.Context, appID types.AppID, since, until time.Time) ([]ModelCostFact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT requested_model, provider, input_tokens, output_tokens,
		       has_tools, has_tool_calls, has_images, json_mode,
		       cost_micro, cost_status
		FROM requests
		WHERE app_id = $1 AND started_at >= $2 AND started_at < $3 AND status = 'ok'
	`, appID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load model-cost facts: %w", err)
	}
	defer rows.Close()

	var facts []ModelCostFact
	for rows.Next() {
		var f ModelCostFact
		var costStatus string
		if err := rows.Scan(&f.RequestedModel, &f.Provider, &f.InputTokens, &f.OutputTokens,
			&f.HasTools, &f.HasToolCalls, &f.HasImages, &f.JSONMode,
			&f.CostMicro, &costStatus); err != nil {
			return nil, fmt.Errorf("store: failed to scan model-cost fact: %w", err)
		}
		f.CostStatus = types.CostStatus(costStatus)
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load model-cost facts: %w", err)
	}
	return facts, nil
}

// ReplayFact is the per-request shape Phase 4's simulation engine
// (internal/sim) replays — a superset of ModelCostFact adding the fields
// a chronological replay needs (StartedAt for ordering, ID for
// idempotent iteration, CacheKey for the exact-caching scenario).
type ReplayFact struct {
	ID             string
	StartedAt      time.Time
	RequestedModel string
	Provider       string
	Model          string
	InputTokens    int
	OutputTokens   int
	CostMicro      int64
	CostStatus     types.CostStatus
	Status         string
	CacheKey       []byte
}

// ReplayFacts returns every request for an application in [since, until),
// ordered chronologically — the raw material internal/sim replays.
// Unlike ModelCostFacts this includes every status (a simulation reports
// on "requests," not just successfully-served ones) and every
// cost_status (Cost recomputation for an unknown-cost request still
// yields CostUnknown, same as production — Part D.2 applies to
// simulation too).
func (r *Requests) ReplayFacts(ctx context.Context, appID types.AppID, since, until time.Time) ([]ReplayFact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, started_at, requested_model, provider, model, input_tokens, output_tokens,
		       cost_micro, cost_status, status, cache_key
		FROM requests
		WHERE app_id = $1 AND started_at >= $2 AND started_at < $3
		ORDER BY started_at, id
	`, appID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load replay facts: %w", err)
	}
	defer rows.Close()

	var facts []ReplayFact
	for rows.Next() {
		var f ReplayFact
		var costStatus string
		if err := rows.Scan(&f.ID, &f.StartedAt, &f.RequestedModel, &f.Provider, &f.Model, &f.InputTokens, &f.OutputTokens,
			&f.CostMicro, &costStatus, &f.Status, &f.CacheKey); err != nil {
			return nil, fmt.Errorf("store: failed to scan replay fact: %w", err)
		}
		f.CostStatus = types.CostStatus(costStatus)
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load replay facts: %w", err)
	}
	return facts, nil
}

// PeriodStats returns the total request count and cost for an
// application over [since, until) — the raw material Phase 6's baseline
// freeze and observed-window comparison both need. Only status='ok'
// requests count, the same population Part G.3.1's detector and Phase
// 4/5's simulations use (Rule 9-adjacent consistency: every subsystem
// that reasons about "real served traffic" agrees on what that means).
func (r *Requests) PeriodStats(ctx context.Context, appID types.AppID, since, until time.Time) (requests int64, costMicro int64, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(cost_micro), 0)
		FROM requests
		WHERE app_id = $1 AND started_at >= $2 AND started_at < $3 AND status = 'ok'
	`, appID, since, until).Scan(&requests, &costMicro)
	if err != nil {
		return 0, 0, fmt.Errorf("store: failed to load period stats: %w", err)
	}
	return requests, costMicro, nil
}

// RepeatedRequestFact is the per-request shape the Repeated Request
// detector (Part G.3.2) reads: enough to group by cache_key, replay
// chronologically through a simulated TTL cache (internal/sim), and
// describe the top duplicated "shapes" in evidence without ever needing
// the request body itself.
type RepeatedRequestFact struct {
	StartedAt        time.Time
	CacheKey         []byte
	CostMicro        int64
	CostStatus       types.CostStatus
	RequestedModel   string
	InputTokens      int
	SystemPromptHash []byte
}

// RepeatedRequestFacts returns every successfully-served, known-cost
// request with a cache key for an application in [since, until), ordered
// chronologically — a request with no cache key (pre-Phase-4 history)
// can never be part of a duplicate group and is excluded up front.
func (r *Requests) RepeatedRequestFacts(ctx context.Context, appID types.AppID, since, until time.Time) ([]RepeatedRequestFact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT started_at, cache_key, cost_micro, cost_status, requested_model, input_tokens, system_prompt_hash
		FROM requests
		WHERE app_id = $1 AND started_at >= $2 AND started_at < $3 AND status = 'ok' AND cache_key IS NOT NULL
		ORDER BY started_at, id
	`, appID, since, until)
	if err != nil {
		return nil, fmt.Errorf("store: failed to load repeated-request facts: %w", err)
	}
	defer rows.Close()

	var facts []RepeatedRequestFact
	for rows.Next() {
		var f RepeatedRequestFact
		var costStatus string
		if err := rows.Scan(&f.StartedAt, &f.CacheKey, &f.CostMicro, &costStatus, &f.RequestedModel, &f.InputTokens, &f.SystemPromptHash); err != nil {
			return nil, fmt.Errorf("store: failed to scan repeated-request fact: %w", err)
		}
		f.CostStatus = types.CostStatus(costStatus)
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: failed to load repeated-request facts: %w", err)
	}
	return facts, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func nullableAPIKeyID(id types.APIKeyID) any {
	if id == "" {
		return nil
	}
	return id
}
