package store

import (
	"context"
	"fmt"

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
		message_count, system_prompt_hash
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7,
		$8, $9, $10,
		$11, $12, $13, $14, $15, $16,
		$17, $18, $19, $20, $21,
		$22, $23, $24, $25, $26,
		$27, $28, $29,
		$30, $31, $32, $33,
		$34, $35, $36, $37, $38, $39,
		$40, $41
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
			rec.MessageCount, nullableBytes(rec.SystemPromptHash),
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
