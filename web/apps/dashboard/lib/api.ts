// Minimal typed fetch client for Fluxen's control API. Phase 1 keeps this
// deliberately small — a handful of functions, no generated client, no
// query-caching library — since the API surface is still just
// setup/auth/applications/keys (Part L Phase 1 API contracts). A
// generated OpenAPI client is a later-phase concern once the surface is
// large enough to justify one (Rule 6: simplest implementation for the
// current phase's needs).
//
// Every call is client-side (browser fetch with credentials: 'include'),
// deliberately avoiding Next.js Server Components for data fetching here —
// SSR would need the *internal* Docker network hostname for the API while
// the browser needs the *public* one, and resolving that dual-URL problem
// is not worth it for Phase 1's minimal UI (see README).

export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(
  path: string,
  init?: RequestInit
): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });

  if (!res.ok) {
    let message = `Request failed with status ${res.status}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // response wasn't JSON — keep the generic message
    }
    throw new ApiError(res.status, message);
  }

  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

function put<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: "PUT",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

function get<T>(path: string): Promise<T> {
  return request<T>(path, { method: "GET" });
}

function del<T>(path: string): Promise<T> {
  return request<T>(path, { method: "DELETE" });
}

// --- setup ---

export interface SetupStatus {
  complete: boolean;
}

export interface SetupRequest {
  org_name: string;
  email: string;
  password: string;
}

export const api = {
  setupStatus: () => get<SetupStatus>("/api/v1/setup"),
  setup: (body: SetupRequest) => post<{ org_id: string; email: string }>("/api/v1/setup", body),

  login: (body: { email: string; password: string }) =>
    post<{ email: string }>("/api/v1/auth/login", body),
  logout: () => post<void>("/api/v1/auth/logout"),
  session: () =>
    get<{ user_id: string; org_id: string; email: string }>(
      "/api/v1/auth/session"
    ),

  listApplications: () => get<Application[]>("/api/v1/applications"),
  createApplication: (body: { name: string }) =>
    post<Application>("/api/v1/applications", body),

  createKey: (appId: string, body: { name: string }) =>
    post<ApiKey>(`/api/v1/applications/${appId}/keys`, body),
  revokeKey: (keyId: string) => del<void>(`/api/v1/keys/${keyId}`),

  applicationSummary: (appId: string, range: RangeValue) =>
    get<ApplicationSummary>(`/api/v1/applications/${appId}/summary?range=${range}`),
  applicationTimeseries: (appId: string, range: RangeValue) =>
    get<DailyPoint[]>(`/api/v1/applications/${appId}/timeseries?range=${range}`),
  applicationModels: (appId: string, range: RangeValue) =>
    get<ModelBreakdown[]>(`/api/v1/applications/${appId}/models?range=${range}`),

  listOpportunities: (params?: { appId?: string; status?: string }) => {
    const q = new URLSearchParams();
    if (params?.appId) q.set("app_id", params.appId);
    if (params?.status) q.set("status", params.status);
    const qs = q.toString();
    return get<Opportunity[]>(`/api/v1/opportunities${qs ? `?${qs}` : ""}`);
  },
  getOpportunity: (id: string) => get<Opportunity>(`/api/v1/opportunities/${id}`),
  reviewOpportunity: (id: string) => post<Opportunity>(`/api/v1/opportunities/${id}/review`),

  createSimulation: (body: CreateSimulationRequest) => post<Simulation>("/api/v1/simulations", body),
  getSimulation: (id: string) => get<Simulation>(`/api/v1/simulations/${id}`),
  listApplicationSimulations: (appId: string) => get<Simulation[]>(`/api/v1/applications/${appId}/simulations`),

  getPolicy: (appId: string) => get<PolicyResponse>(`/api/v1/applications/${appId}/policy`),
  putPolicy: (appId: string, body: PutPolicyRequest) => put<PolicyResponse>(`/api/v1/applications/${appId}/policy`, body),
  getPolicyHistory: (appId: string) => get<PolicyHistoryEntry[]>(`/api/v1/applications/${appId}/policy/history`),
  revertPolicy: (appId: string, body: { version: number; note?: string }) =>
    post<PolicyResponse>(`/api/v1/applications/${appId}/policy/revert`, body),

  applyOpportunity: (opportunityId: string, body: ApplyOpportunityRequest) =>
    post<PolicyResponse>(`/api/v1/opportunities/${opportunityId}/apply`, body),

  listMeasurements: (appId: string) => get<Measurement[]>(`/api/v1/measurements?app_id=${appId}`),
  getMeasurement: (id: string) => get<Measurement>(`/api/v1/measurements/${id}`),
  getOpportunityMeasurement: (opportunityId: string) =>
    get<Measurement>(`/api/v1/opportunities/${opportunityId}/measurement`),
  revertMeasurement: (id: string, body: { confirm: boolean; note?: string }) =>
    post<PolicyResponse>(`/api/v1/measurements/${id}/revert`, body),

  // --- Phase 7 additions ---
  overview: (range: RangeValue) => get<OverviewResponse>(`/api/v1/overview?range=${range}`),
  overviewTimeseries: (range: RangeValue) =>
    get<OverviewDailyPoint[]>(`/api/v1/overview/timeseries?range=${range}`),

  applicationScore: (appId: string) => get<ScoreResponse>(`/api/v1/applications/${appId}/score`),
  applicationScoreHistory: (appId: string, range: RangeValue) =>
    get<ScoreResponse[]>(`/api/v1/applications/${appId}/score/history?range=${range}`),

  listRequests: (params?: {
    appId?: string;
    model?: string;
    status?: string;
    cache?: string;
    cursor?: string;
  }) => {
    const q = new URLSearchParams();
    if (params?.appId) q.set("app_id", params.appId);
    if (params?.model) q.set("model", params.model);
    if (params?.status) q.set("status", params.status);
    if (params?.cache) q.set("cache", params.cache);
    if (params?.cursor) q.set("cursor", params.cursor);
    const qs = q.toString();
    return get<RequestListResponse>(`/api/v1/requests${qs ? `?${qs}` : ""}`);
  },
  getRequest: (id: string) => get<RequestDetail>(`/api/v1/requests/${id}`),

  // --- Settings (Part I.6) ---
  listProviderCredentials: () => get<ProviderCredential[]>("/api/v1/providers/credentials"),
  createProviderCredential: (body: CreateProviderCredentialRequest) =>
    post<ProviderCredential>("/api/v1/providers/credentials", body),
  revokeProviderCredential: (id: string) =>
    post<ProviderCredential>(`/api/v1/providers/credentials/${id}/revoke`),
  providerHealthCheck: (id: string) =>
    post<{ status: string; error: string }>(`/api/v1/providers/${id}/health-check`),
  listProviderModels: (provider: string) =>
    get<{ ID: string }[]>(`/api/v1/providers/${provider}/models`),

  getPricingCatalog: () => get<PricingCatalog>("/api/v1/pricing/catalog"),

  getSettings: () => get<Settings>("/api/v1/settings"),
  patchSettings: (body: Settings) => request<Settings>("/api/v1/settings", { method: "PATCH", body: JSON.stringify(body) }),

  listUsers: () => get<User[]>("/api/v1/users"),
  inviteUser: (body: { email: string; role: "owner" | "member" }) =>
    post<InvitedUser>("/api/v1/users", body),
};

// Matches the ?range= values internal/api/timerange.go accepts.
export type RangeValue = "24h" | "7d" | "30d" | "90d";

export interface ApplicationSummary {
  range_start: string;
  range_end: string;
  requests: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_micro: number;
  avg_duration_ms: number;
}

export interface DailyPoint {
  day: string;
  requests: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_micro: number;
  avg_duration_ms: number;
}

export interface ModelBreakdown {
  provider: string;
  model: string;
  requests: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_micro: number;
  avg_duration_ms: number;
}

export interface Application {
  id: string;
  slug: string;
  name: string;
  status: string;
  created_at: string;
  first_seen_at?: string;
  last_seen_at?: string;
}

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  key: string;
  created_at: string;
}

// Opportunity mirrors internal/api's opportunityResponse (Part L Phase 3).
// evidence/recommendation are passed through as untyped JSON on the wire —
// ModelCostEvidence/ModelCostRecommendation narrow them for the one kind
// Phase 3 ships; a later phase's detector kinds would add their own
// narrowing types alongside these, not replace this shape.
export interface Opportunity {
  id: string;
  app_id: string;
  kind: string;
  status: string;
  severity?: string;
  title: string;
  summary: string;
  window_start: string;
  window_end: string;
  sample_requests: number;
  current_cost_micro: number;
  projected_cost_micro: number;
  savings_micro: number;
  savings_pct: number;
  value_type: string;
  confidence: "low" | "medium" | "high";
  confidence_score: number;
  evidence: unknown;
  recommendation: unknown;
  detector_version: string;
  detected_at: string;
  reviewed_at?: string;
  dismissed_at?: string;
  dismiss_reason?: string;
  last_seen_at: string;
}

export interface ModelCostEvidence {
  current_model: string;
  candidate_model: string;
  total_requests: number;
  eligible_requests: number;
  eligible_fraction: number;
  exclusion_breakdown: Record<string, number>;
  input_tokens_p50: number;
  input_tokens_p95: number;
  output_tokens_p50: number;
  output_tokens_p95: number;
  price_ratio: number;
  window_days: number;
  caveat: string;
}

export interface ModelCostRecommendation {
  action: string;
  current_model: string;
  candidate_model: string;
  recommended_traffic_weight: number;
}

// Simulation mirrors internal/api's simulationResponse (Part L Phase 4).
// scenario is sent as one of the two typed request shapes below; the
// response's `scenario` field passes through whatever was stored,
// untyped, same convention as Opportunity.evidence/recommendation.
export interface Simulation {
  id: string;
  app_id: string;
  opportunity_id?: string;
  scenario: unknown;
  window_start: string;
  window_end: string;
  replayed_requests: number;
  affected_requests: number;
  sampled: boolean;
  actual_cost_micro: number;
  simulated_cost_micro: number;
  delta_micro: number;
  delta_pct: number;
  projected_monthly_savings_micro: number;
  value_type: string;
  breakdown: unknown;
  assumptions: string[];
  engine_version: string;
  created_at: string;
}

export interface ModelMixScenarioRequest {
  type: "model_mix";
  current_model: string;
  candidate_model: string;
  traffic_weight: number;
}

export interface CachingScenarioRequest {
  type: "exact_caching";
  ttl_seconds: number;
  max_entries?: number;
}

export interface CreateSimulationRequest {
  app_id: string;
  opportunity_id?: string;
  window_days?: number;
  scenario: ModelMixScenarioRequest | CachingScenarioRequest;
}

export interface ModelMixBreakdownRow {
  model: string;
  requests: number;
  actual_cost_micro: number;
  simulated_cost_micro: number;
}

// PolicyDocument mirrors pkg/policy.PolicyDocument (Part L Phase 5) —
// every control optional and nil/disabled by default, matching the
// gateway's own "no policy = Phase 1 behavior" semantics.
export interface RoutingPolicy {
  enabled: boolean;
  from_model: string;
  to_model: string;
  weight: number;
  sticky: boolean;
}
export interface CachingPolicy {
  enabled: boolean;
  ttl_seconds: number;
}
export interface BudgetPolicy {
  enabled: boolean;
  period: "daily" | "monthly";
  limit_micro: number;
  mode: "hard" | "soft";
}
export interface RateLimitPolicy {
  enabled: boolean;
  requests_per_minute: number;
}
export interface ModelRestrictionPolicy {
  enabled: boolean;
  allowed_models: string[];
}
export interface PolicyDocument {
  routing?: RoutingPolicy | null;
  caching?: CachingPolicy | null;
  budget?: BudgetPolicy | null;
  rate_limit?: RateLimitPolicy | null;
  model_restriction?: ModelRestrictionPolicy | null;
}

export interface PolicyResponse {
  app_id: string;
  version: number;
  document: PolicyDocument;
  updated_at: string;
}

export interface PutPolicyRequest {
  document: PolicyDocument;
  expected_version: number;
  note?: string;
}

export interface PolicyHistoryEntry {
  id: string;
  version: number;
  document: PolicyDocument;
  diff: Record<string, { old: unknown; new: unknown }>;
  change_source: "user" | "opportunity" | "revert";
  opportunity_id?: string;
  simulation_id?: string;
  note?: string;
  changed_at: string;
}

export interface ApplyOpportunityRequest {
  confirm: boolean;
  simulation_id: string;
  routing: { from_model: string; to_model: string; weight: number; sticky: boolean };
  note?: string;
}

// Measurement mirrors internal/api's measurementResponse (Part L Phase
// 6). Baseline/observed figures are "measured" (real recorded traffic);
// actual_savings_micro/actual_pct only exist once status is "final" —
// that's "realized," Part G.6's fourth and last value type.
export type MeasurementStatus = "collecting" | "interim" | "final" | "reverted";
export type Verdict = "successful" | "partial" | "no_effect" | "regressed" | "inconclusive";

export interface Measurement {
  id: string;
  app_id: string;
  opportunity_id: string;
  simulation_id?: string;
  policy_version: number;
  applied_at: string;

  baseline_start: string;
  baseline_end: string;
  baseline_requests: number;
  baseline_cost_micro: number;
  baseline_cost_per_1k_micro: number;

  observed_start?: string;
  observed_end?: string;
  observed_requests?: number;
  observed_cost_micro?: number;
  observed_cost_per_1k_micro?: number;

  expected_savings_micro: number;
  actual_savings_micro?: number;
  expected_pct: number;
  actual_pct?: number;

  verdict?: Verdict;
  verdict_reason?: string;

  status: MeasurementStatus;
  finalized_at?: string;
}

// --- Phase 7: repeated request / token efficiency / traffic anomaly
// evidence (Part G.3.2-G.3.4), mirroring internal/detect's exported
// shapes field-for-field. ---

export interface TTLSweepRow {
  ttl: string;
  duplicate_hits: number;
  savings_micro: number;
}
export interface DuplicateShape {
  system_prompt_hash: string;
  model: string;
  input_tokens_p50: number;
  count: number;
}
export interface RepeatedRequestEvidence {
  total_requests: number;
  duplicate_requests: number;
  duplicate_rate: number;
  ttl_sweep: TTLSweepRow[];
  top_shapes: DuplicateShape[];
  window_days: number;
}
export interface RepeatedRequestRecommendation {
  action: string;
  recommended_ttl_seconds: number;
}

export interface DailyTokenPoint {
  day: string;
  mean_tokens: number;
}
export interface TokenEfficiencyEvidence {
  dimension: "input" | "output";
  model: string;
  baseline_mean_tokens: number;
  current_mean_tokens: number;
  drift_pct: number;
  change_point_date?: string;
  daily_sparkline: DailyTokenPoint[];
  baseline_requests: number;
  current_requests: number;
}

export interface AnomalyHourPoint {
  bucket: string;
  requests: number;
  cost_micro: number;
  total_tokens: number;
  error_rate: number;
  z_score: number;
}
export interface TrafficAnomalyEvidence {
  metric: "requests" | "cost" | "tokens" | "error_rate";
  peak_z_score: number;
  baseline_median: number;
  peak_value: number;
  error_rate_spike_pct: number;
  hourly_series: AnomalyHourPoint[];
}

// --- Phase 7: Efficiency Score (internal/score) ---

export interface ScoreResponse {
  day: string;
  status: "ok" | "insufficient_data";
  overall: number;
  model_efficiency: number;
  token_efficiency: number;
  cache_efficiency: number;
  traffic_stability: number;
  cost_efficiency: number;
}

// --- Phase 7: Overview (Part I.3) ---

export interface OverviewProviderMix {
  provider: string;
  cost_micro: number;
  requests: number;
}
export interface OverviewTopApplication {
  app_id: string;
  slug: string;
  name: string;
  cost_micro: number;
  prior_cost_micro: number;
  efficiency_score: number | null;
  open_opportunity_value_micro: number;
}
export interface OverviewOpportunityRow {
  id: string;
  app_id: string;
  kind: string;
  title: string;
  savings_micro: number;
  savings_pct: number;
  confidence: "low" | "medium" | "high";
  confidence_score: number;
}
export interface OverviewResponse {
  range_start: string;
  range_end: string;
  requests: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_micro: number;
  potential_savings_micro: number;
  realized_savings_micro: number;
  provider_mix: OverviewProviderMix[];
  top_applications: OverviewTopApplication[];
  opportunity_feed: OverviewOpportunityRow[];
}
export interface OverviewDailyPoint {
  day: string;
  cost_micro: number;
  requests: number;
}

// --- Phase 7: Requests investigation (Part I.5) ---

export interface RequestRow {
  id: string;
  app_id: string;
  started_at: string;
  duration_ms: number;
  requested_model: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_micro: number;
  cost_status: "known" | "unknown" | "local";
  cache_status: "hit" | "miss" | "bypass" | "disabled";
  status: string;
  http_status: number | null;
}
export interface RequestListResponse {
  requests: RequestRow[];
  next_cursor: string;
}
export interface RequestDetail extends RequestRow {
  ttft_ms: number | null;
  endpoint: string;
  protocol: string;
  streamed: boolean;
  route_reason: string;
  route_variant: string | null;
  policy_version: number;
  cached_input_tokens: number;
  usage_source: string;
  cost_input_micro: number;
  cost_output_micro: number;
  pricing_version: string;
  cache_saved_micro: number;
  error_code: string | null;
  error_message: string | null;
  has_tools: boolean;
  has_tool_calls: boolean;
  has_images: boolean;
  json_mode: boolean;
  temperature: number | null;
  max_tokens_req: number | null;
  message_count: number | null;
  request_body?: unknown;
  response_body?: unknown;
}

// --- Phase 7: Settings (Part I.6) ---

export interface ProviderCredential {
  id: string;
  provider: "openai" | "gemini" | "ollama";
  base_url: string | null;
  status: "active" | "revoked";
  created_at: string;
  revoked_at?: string;
  last_health_check_at?: string;
  last_health_check_status?: "ok" | "error";
  last_health_check_error?: string;
}
export interface CreateProviderCredentialRequest {
  provider: "openai" | "gemini" | "ollama";
  api_key?: string;
  base_url?: string;
}

export interface PricingModel {
  id: string;
  provider: string;
  input_per_mtok_micro: number;
  output_per_mtok_micro: number;
  context_window: number;
  max_output: number;
  capabilities: string[];
  tier: string;
  downgrade_candidates_for?: string[];
}
export interface PricingCatalog {
  version: string;
  models: PricingModel[];
  ollama_note: string;
}

export interface Settings {
  requests_retention_days: number;
  body_retention_days: number;
  body_capture_enabled: boolean;
}

export interface User {
  id: string;
  email: string;
  role: "owner" | "member";
  created_at: string;
}
export interface InvitedUser extends User {
  temporary_password: string;
}

// The gateway itself (not the control API) — what applications actually
// point their OpenAI client's base_url at.
export const GATEWAY_BASE_URL =
  process.env.NEXT_PUBLIC_GATEWAY_BASE_URL ?? "http://localhost:8080/v1";
