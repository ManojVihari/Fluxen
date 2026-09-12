"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  api,
  ApiError,
  type Application,
  type RequestDetail,
  type RequestRow,
} from "@/lib/api";
import { Badge, Button, EmptyState, ErrorBanner } from "@/components/ui";
import { formatMoney, formatNumber } from "@/lib/format";

const selectClass =
  "rounded-md border border-slate-300 bg-white px-2.5 py-1.5 text-sm text-slate-700 focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500";

const STATUS_OPTIONS = ["ok", "provider_error", "timeout", "blocked", "client_abort"];
const CACHE_OPTIONS = ["hit", "miss", "bypass", "disabled"];

// RequestsTable is Part I.5's investigation table — filter, cursor-
// paginated ("load more," matching the API's forward-only cursor rather
// than a page-number scheme), and a detail drawer. Shared between the
// top-level /requests screen (every application, an app filter shown)
// and Application Detail's Requests tab (appId fixed, no app filter).
export function RequestsTable({ appId }: { appId?: string }) {
  const router = useRouter();

  const [apps, setApps] = useState<Application[]>([]);
  const [appFilter, setAppFilter] = useState(appId ?? "");
  const [model, setModel] = useState("");
  const [status, setStatus] = useState("");
  const [cache, setCache] = useState("");

  const [rows, setRows] = useState<RequestRow[] | null>(null);
  const [nextCursor, setNextCursor] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    if (appId) return; // fixed to one application — no picker needed
    api.listApplications().then(setApps).catch(() => {});
  }, [appId]);

  function load(cursor?: string) {
    if (cursor) setLoadingMore(true);
    else {
      setRows(null);
      setError(null);
    }
    api
      .listRequests({
        appId: appFilter || undefined,
        model: model || undefined,
        status: status || undefined,
        cache: cache || undefined,
        cursor,
      })
      .then((resp) => {
        setRows((prev) => (cursor && prev ? [...prev, ...resp.requests] : resp.requests));
        setNextCursor(resp.next_cursor);
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load requests.");
      })
      .finally(() => setLoadingMore(false));
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [appFilter, model, status, cache]);

  return (
    <div>
      <div className="mb-4 flex flex-wrap gap-2">
        {!appId && (
          <select className={selectClass} value={appFilter} onChange={(e) => setAppFilter(e.target.value)}>
            <option value="">All applications</option>
            {apps.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        )}
        <input
          className={selectClass}
          placeholder="Filter by model…"
          value={model}
          onChange={(e) => setModel(e.target.value)}
        />
        <select className={selectClass} value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">All statuses</option>
          {STATUS_OPTIONS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <select className={selectClass} value={cache} onChange={(e) => setCache(e.target.value)}>
          <option value="">Any cache status</option>
          {CACHE_OPTIONS.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
        {(model || status || cache || (!appId && appFilter)) && (
          <button
            onClick={() => {
              setModel("");
              setStatus("");
              setCache("");
              if (!appId) setAppFilter("");
            }}
            className="text-xs font-medium text-slate-500 underline-offset-2 hover:underline"
          >
            Clear filters
          </button>
        )}
      </div>

      <ErrorBanner message={error} />

      {rows === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {rows?.length === 0 && <EmptyState>No requests match these filters.</EmptyState>}

      {rows && rows.length > 0 && (
        <>
          <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                  <th className="px-3 py-2 font-medium">Time</th>
                  {!appId && <th className="px-3 py-2 font-medium">App</th>}
                  <th className="px-3 py-2 font-medium">Model</th>
                  <th className="px-3 py-2 font-medium text-right">Tokens</th>
                  <th className="px-3 py-2 font-medium text-right">Cost</th>
                  <th className="px-3 py-2 font-medium text-right">Latency</th>
                  <th className="px-3 py-2 font-medium">Cache</th>
                  <th className="px-3 py-2 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr
                    key={r.id}
                    onClick={() => setSelectedId(r.id)}
                    className="cursor-pointer border-b border-slate-100 last:border-0 hover:bg-slate-50"
                  >
                    <td className="px-3 py-2 text-xs text-slate-500">
                      {new Date(r.started_at).toLocaleString()}
                    </td>
                    {!appId && <td className="px-3 py-2 text-xs text-slate-500">{r.app_id.slice(0, 8)}</td>}
                    <td className="px-3 py-2">
                      {r.requested_model !== r.model ? (
                        <span>
                          {r.requested_model} <span className="text-slate-400">→</span> {r.model}
                        </span>
                      ) : (
                        r.model
                      )}
                    </td>
                    <td className="px-3 py-2 text-right">{formatNumber(r.total_tokens)}</td>
                    <td className="px-3 py-2 text-right">
                      {r.cost_status === "local" ? (
                        <span className="text-slate-400">Local · not billed</span>
                      ) : r.cost_status === "unknown" ? (
                        <span className="text-slate-400">Cost unknown</span>
                      ) : (
                        formatMoney(r.cost_micro)
                      )}
                    </td>
                    <td className="px-3 py-2 text-right">{r.duration_ms}ms</td>
                    <td className="px-3 py-2">
                      <Badge tone={r.cache_status === "hit" ? "good" : "neutral"}>{r.cache_status}</Badge>
                    </td>
                    <td className="px-3 py-2">
                      <Badge tone={r.status === "ok" ? "good" : r.status === "blocked" ? "warn" : "warn"}>
                        {r.status}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {nextCursor && (
            <div className="mt-3 text-center">
              <Button variant="secondary" onClick={() => load(nextCursor)} disabled={loadingMore}>
                {loadingMore ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </>
      )}

      {selectedId && <RequestDetailDrawer id={selectedId} onClose={() => setSelectedId(null)} />}
    </div>
  );
}

function RequestDetailDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const [detail, setDetail] = useState<RequestDetail | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setDetail(null);
    setError(null);
    api
      .getRequest(id)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof ApiError ? err.message : "Failed to load request.");
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-slate-900/20" onClick={onClose}>
      <div
        className="h-full w-full max-w-md overflow-y-auto border-l border-slate-200 bg-white p-6 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-900">Request detail</h2>
          <button onClick={onClose} className="text-sm text-slate-400 hover:text-slate-700">
            ✕
          </button>
        </div>

        <ErrorBanner message={error} />
        {!detail && !error && <p className="text-sm text-slate-500">Loading…</p>}

        {detail && (
          <div className="space-y-4 text-sm">
            <DetailRow label="ID" value={detail.id} mono />
            <DetailRow label="Started" value={new Date(detail.started_at).toLocaleString()} />
            <DetailRow label="Endpoint / protocol" value={`${detail.endpoint} · ${detail.protocol}`} />
            <DetailRow label="Streamed" value={detail.streamed ? "yes" : "no"} />
            <DetailRow
              label="Model"
              value={
                detail.requested_model !== detail.model
                  ? `${detail.requested_model} → ${detail.model} (${detail.provider})`
                  : `${detail.model} (${detail.provider})`
              }
            />
            <DetailRow label="Route reason" value={`${detail.route_reason}${detail.route_variant ? ` (${detail.route_variant})` : ""}`} />
            <DetailRow label="Policy version" value={String(detail.policy_version)} />
            <DetailRow
              label="Tokens"
              value={`${formatNumber(detail.input_tokens)} in / ${formatNumber(detail.output_tokens)} out / ${formatNumber(detail.cached_input_tokens)} cached`}
            />
            <DetailRow
              label="Cost"
              value={
                detail.cost_status === "local"
                  ? "Local · not billed"
                  : detail.cost_status === "unknown"
                    ? "Cost unknown"
                    : `${formatMoney(detail.cost_micro)} (${formatMoney(detail.cost_input_micro)} in / ${formatMoney(detail.cost_output_micro)} out)`
              }
            />
            <DetailRow label="Cache" value={`${detail.cache_status}${detail.cache_saved_micro > 0 ? ` · saved ${formatMoney(detail.cache_saved_micro)}` : ""}`} />
            <DetailRow label="Latency" value={`${detail.duration_ms}ms${detail.ttft_ms != null ? ` (TTFT ${detail.ttft_ms}ms)` : ""}`} />
            <DetailRow label="Status" value={`${detail.status}${detail.http_status != null ? ` (${detail.http_status})` : ""}`} />
            {detail.error_code && <DetailRow label="Error" value={`${detail.error_code}: ${detail.error_message ?? ""}`} />}
            <DetailRow
              label="Shape"
              value={[
                detail.has_tools && "tools",
                detail.has_tool_calls && "tool_calls",
                detail.has_images && "images",
                detail.json_mode && "json_mode",
              ]
                .filter(Boolean)
                .join(", ") || "none"}
            />
            <DetailRow
              label="Body capture"
              value={
                detail.request_body || detail.response_body
                  ? "captured (unredacted — see Settings → Retention)"
                  : "not captured (capture was off, or this was a streamed response)"
              }
            />
          </div>
        )}
      </div>
    </div>
  );
}

function DetailRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-slate-400">{label}</p>
      <p className={`mt-0.5 text-slate-800 ${mono ? "break-all font-mono text-xs" : ""}`}>{value}</p>
    </div>
  );
}
