package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"fluxen/internal/store"
	"fluxen/pkg/types"
)

type requestRowResponse struct {
	ID             string `json:"id"`
	AppID          string `json:"app_id"`
	StartedAt      string `json:"started_at"`
	DurationMS     int    `json:"duration_ms"`
	RequestedModel string `json:"requested_model"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
	TotalTokens    int    `json:"total_tokens"`
	CostMicro      int64  `json:"cost_micro"`
	CostStatus     string `json:"cost_status"`
	CacheStatus    string `json:"cache_status"`
	Status         string `json:"status"`
	HTTPStatus     *int   `json:"http_status"`
}

func toRequestRowResponse(r store.RequestRow) requestRowResponse {
	return requestRowResponse{
		ID: r.ID, AppID: string(r.AppID), StartedAt: r.StartedAt.Format(time.RFC3339),
		DurationMS: r.DurationMS, RequestedModel: r.RequestedModel, Provider: r.Provider, Model: r.Model,
		InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, TotalTokens: r.TotalTokens,
		CostMicro: r.CostMicro, CostStatus: r.CostStatus, CacheStatus: r.CacheStatus,
		Status: r.Status, HTTPStatus: r.HTTPStatus,
	}
}

// handleListRequests is Part I.5's investigation listing:
// GET /api/v1/requests?app_id=&model=&status=&cache=&from=&to=&cursor=,
// cursor-paginated. Unlike every other list endpoint this reads the
// `requests` fact table directly — Part I.5 is explicitly the one screen
// allowed to.
func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	q := r.URL.Query()

	filter := store.RequestsFilter{
		AppID: types.AppID(q.Get("app_id")), Model: q.Get("model"),
		Status: q.Get("status"), CacheStatus: q.Get("cache"), Cursor: q.Get("cursor"),
	}
	if from := q.Get("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid \"from\" parameter, expected RFC3339")
			return
		}
		filter.From = t
	}
	if to := q.Get("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid \"to\" parameter, expected RFC3339")
			return
		}
		filter.Until = t
	}

	// app_id, if given, must belong to the caller's org — the same
	// ownership check every per-application endpoint applies, done here
	// via a lookup rather than mustOwnApplication since a missing app_id
	// is valid (it means "every application").
	if filter.AppID != "" {
		if _, err := s.Apps.Get(r.Context(), uc.OrgID, filter.AppID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "application not found")
				return
			}
			s.Logger.Error("api: failed to look up application for requests filter", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	rows, nextCursor, err := s.Requests.List(r.Context(), uc.OrgID, filter)
	if err != nil {
		s.Logger.Error("api: failed to list requests", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]requestRowResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRequestRowResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out, "next_cursor": nextCursor})
}

type requestDetailResponse struct {
	requestRowResponse
	TTFTMS            *int            `json:"ttft_ms"`
	Endpoint          string          `json:"endpoint"`
	Protocol          string          `json:"protocol"`
	Streamed          bool            `json:"streamed"`
	RouteReason       string          `json:"route_reason"`
	RouteVariant      *string         `json:"route_variant"`
	PolicyVersion     int             `json:"policy_version"`
	CachedInputTokens int             `json:"cached_input_tokens"`
	UsageSource       string          `json:"usage_source"`
	CostInputMicro    int64           `json:"cost_input_micro"`
	CostOutputMicro   int64           `json:"cost_output_micro"`
	PricingVersion    string          `json:"pricing_version"`
	CacheSavedMicro   int64           `json:"cache_saved_micro"`
	ErrorCode         *string         `json:"error_code"`
	ErrorMessage      *string         `json:"error_message"`
	HasTools          bool            `json:"has_tools"`
	HasToolCalls      bool            `json:"has_tool_calls"`
	HasImages         bool            `json:"has_images"`
	JSONMode          bool            `json:"json_mode"`
	Temperature       *float64        `json:"temperature"`
	MaxTokensReq      *int            `json:"max_tokens_req"`
	MessageCount      *int            `json:"message_count"`
	RequestBody       json.RawMessage `json:"request_body,omitempty"`
	ResponseBody      json.RawMessage `json:"response_body,omitempty"`
}

// handleGetRequest is Part I.5's detail drawer: full metadata, routing
// decision, policy version at request time, and request/response bodies
// when the org had body capture enabled (Settings > Retention, off by
// default) at the time the request was proxied — the gateway itself
// decides whether to populate these columns per request; this handler
// only reads whatever ended up there. Captured bodies are stored
// unredacted, and only for non-streaming responses.
func (s *Server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	uc, _ := userFromRequest(r)
	id := chi.URLParam(r, "requestID")

	d, err := s.Requests.Get(r.Context(), uc.OrgID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "request not found")
			return
		}
		s.Logger.Error("api: failed to load request detail", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, requestDetailResponse{
		requestRowResponse: toRequestRowResponse(d.RequestRow),
		TTFTMS:             d.TTFTMS, Endpoint: d.Endpoint, Protocol: d.Protocol, Streamed: d.Streamed,
		RouteReason: d.RouteReason, RouteVariant: d.RouteVariant, PolicyVersion: d.PolicyVersion,
		CachedInputTokens: d.CachedInputTokens, UsageSource: d.UsageSource,
		CostInputMicro: d.CostInputMicro, CostOutputMicro: d.CostOutputMicro, PricingVersion: d.PricingVersion,
		CacheSavedMicro: d.CacheSavedMicro, ErrorCode: d.ErrorCode, ErrorMessage: d.ErrorMessage,
		HasTools: d.HasTools, HasToolCalls: d.HasToolCalls, HasImages: d.HasImages, JSONMode: d.JSONMode,
		Temperature: d.Temperature, MaxTokensReq: d.MaxTokensReq, MessageCount: d.MessageCount,
		RequestBody: json.RawMessage(d.RequestBody), ResponseBody: json.RawMessage(d.ResponseBody),
	})
}
