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

// The gateway itself (not the control API) — what applications actually
// point their OpenAI client's base_url at.
export const GATEWAY_BASE_URL =
  process.env.NEXT_PUBLIC_GATEWAY_BASE_URL ?? "http://localhost:8080/v1";
