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

// The gateway itself (not the control API) — what applications actually
// point their OpenAI client's base_url at.
export const GATEWAY_BASE_URL =
  process.env.NEXT_PUBLIC_GATEWAY_BASE_URL ?? "http://localhost:8080/v1";
