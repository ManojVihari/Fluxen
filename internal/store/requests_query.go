package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"fluxen/pkg/types"
)

// RequestRow is one row of the Requests investigation screen (Part I.5)
// — a read of the `requests` fact table directly, the one screen in
// Fluxen explicitly allowed to (every other screen reads a rollup).
type RequestRow struct {
	ID             string
	AppID          types.AppID
	StartedAt      time.Time
	DurationMS     int
	RequestedModel string
	Provider       string
	Model          string
	InputTokens    int
	OutputTokens   int
	TotalTokens    int
	CostMicro      int64
	CostStatus     string
	CacheStatus    string
	Status         string
	HTTPStatus     *int
}

// RequestDetail is the full row Part I.5's detail drawer shows: every
// RequestRow field plus routing/policy metadata and the request/response
// bodies (nil unless body capture was enabled for the application at
// request time — that toggle is a Settings feature this pass does not
// build the write side of; the column already exists and this read path
// surfaces whatever is there, honestly null today).
type RequestDetail struct {
	RequestRow
	TTFTMS            *int
	Endpoint          string
	Protocol          string
	Streamed          bool
	RouteReason       string
	RouteVariant      *string
	PolicyVersion     int
	CachedInputTokens int
	UsageSource       string
	CostInputMicro    int64
	CostOutputMicro   int64
	PricingVersion    string
	CacheSavedMicro   int64
	ErrorCode         *string
	ErrorMessage      *string
	HasTools          bool
	HasToolCalls      bool
	HasImages         bool
	JSONMode          bool
	Temperature       *float64
	MaxTokensReq      *int
	MessageCount      *int
	RequestBody       []byte
	ResponseBody      []byte
}

// RequestsFilter is Part I.5's exact filter set:
// ?app_id=&model=&status=&cache=&from=&to=&cursor=.
type RequestsFilter struct {
	AppID       types.AppID
	Model       string
	Status      string
	CacheStatus string
	From, Until time.Time
	Cursor      string
	Limit       int
}

const requestsPageDefaultLimit = 50

var maxCursor = struct {
	ts time.Time
	id string
}{ts: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), id: "ffffffff-ffff-ffff-ffff-ffffffffffff"}

// encodeRequestCursor/decodeRequestCursor make the (started_at, id) seek
// key opaque on the wire — a keyset cursor (not OFFSET) so pagination
// stays cheap regardless of how deep the investigation goes, and stable
// against concurrent inserts ahead of the cursor.
func encodeRequestCursor(startedAt time.Time, id string) string {
	raw := fmt.Sprintf("%s|%s", startedAt.Format(time.RFC3339Nano), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeRequestCursor(cursor string) (time.Time, string, bool) {
	if cursor == "" {
		return maxCursor.ts, maxCursor.id, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", false
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", false
	}
	return ts, parts[1], true
}

// Requests.List (defined alongside the write-path Requests type) is the
// Requests investigation screen's cursor-paginated read.
func (r *Requests) List(ctx context.Context, orgID types.OrgID, f RequestsFilter) (rows []RequestRow, nextCursor string, err error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = requestsPageDefaultLimit
	}
	cursorTS, cursorID, ok := decodeRequestCursor(f.Cursor)
	if !ok {
		return nil, "", fmt.Errorf("store: invalid cursor")
	}

	pgRows, err := r.pool.Query(ctx, `
		SELECT id, app_id, started_at, duration_ms, requested_model, provider, model,
		       input_tokens, output_tokens, total_tokens, cost_micro, cost_status, cache_status, status, http_status
		FROM requests
		WHERE org_id = $1
		  AND ($2 = '' OR app_id::text = $2)
		  AND ($3 = '' OR model = $3)
		  AND ($4 = '' OR status = $4)
		  AND ($5 = '' OR cache_status = $5)
		  AND ($6::timestamptz IS NULL OR started_at >= $6)
		  AND ($7::timestamptz IS NULL OR started_at < $7)
		  AND (started_at, id) < ($8, $9)
		ORDER BY started_at DESC, id DESC
		LIMIT $10
	`, orgID, string(f.AppID), f.Model, f.Status, f.CacheStatus, nullableTime(f.From), nullableTime(f.Until), cursorTS, cursorID, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("store: failed to list requests: %w", err)
	}
	defer pgRows.Close()

	for pgRows.Next() {
		var row RequestRow
		if err := pgRows.Scan(&row.ID, &row.AppID, &row.StartedAt, &row.DurationMS, &row.RequestedModel, &row.Provider, &row.Model,
			&row.InputTokens, &row.OutputTokens, &row.TotalTokens, &row.CostMicro, &row.CostStatus, &row.CacheStatus, &row.Status, &row.HTTPStatus); err != nil {
			return nil, "", fmt.Errorf("store: failed to scan request row: %w", err)
		}
		rows = append(rows, row)
	}
	if err := pgRows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: failed to list requests: %w", err)
	}

	if len(rows) > limit {
		last := rows[limit-1]
		nextCursor = encodeRequestCursor(last.StartedAt, last.ID)
		rows = rows[:limit]
	}
	return rows, nextCursor, nil
}

// Get fetches one request's full detail, scoped to an org so one org can
// never read another's request by guessing an id.
func (r *Requests) Get(ctx context.Context, orgID types.OrgID, id string) (RequestDetail, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, app_id, started_at, duration_ms, ttft_ms, endpoint, protocol, streamed,
		       requested_model, provider, model, route_reason, route_variant, policy_version,
		       input_tokens, output_tokens, cached_input_tokens, total_tokens, usage_source,
		       cost_micro, cost_input_micro, cost_output_micro, cost_status, pricing_version,
		       cache_status, cache_saved_micro,
		       status, http_status, error_code, error_message,
		       has_tools, has_tool_calls, has_images, json_mode, temperature, max_tokens, message_count,
		       request_body, response_body
		FROM requests
		WHERE org_id = $1 AND id = $2
	`, orgID, id)

	var d RequestDetail
	err := row.Scan(
		&d.ID, &d.AppID, &d.StartedAt, &d.DurationMS, &d.TTFTMS, &d.Endpoint, &d.Protocol, &d.Streamed,
		&d.RequestedModel, &d.Provider, &d.Model, &d.RouteReason, &d.RouteVariant, &d.PolicyVersion,
		&d.InputTokens, &d.OutputTokens, &d.CachedInputTokens, &d.TotalTokens, &d.UsageSource,
		&d.CostMicro, &d.CostInputMicro, &d.CostOutputMicro, &d.CostStatus, &d.PricingVersion,
		&d.CacheStatus, &d.CacheSavedMicro,
		&d.Status, &d.HTTPStatus, &d.ErrorCode, &d.ErrorMessage,
		&d.HasTools, &d.HasToolCalls, &d.HasImages, &d.JSONMode, &d.Temperature, &d.MaxTokensReq, &d.MessageCount,
		&d.RequestBody, &d.ResponseBody,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return RequestDetail{}, ErrNotFound
		}
		return RequestDetail{}, fmt.Errorf("store: failed to load request: %w", err)
	}
	return d, nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
