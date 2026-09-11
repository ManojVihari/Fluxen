"use client";

import { Suspense, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, ApiError, type ModelBreakdown } from "@/lib/api";
import { EmptyState, ErrorBanner } from "@/components/ui";
import { RangePicker, useRangeParam } from "@/components/range-picker";
import { formatDuration, formatMoney, formatNumber, formatPercent } from "@/lib/format";

// Application Detail's Models tab — "which models/providers are
// responsible" (Part L Phase 2 exit criteria), sorted by cost so the
// biggest driver leads.
export default function ModelsPage() {
  return (
    <Suspense fallback={<p className="text-sm text-slate-500">Loading…</p>}>
      <ModelsContent />
    </Suspense>
  );
}

function ModelsContent() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const range = useRangeParam();

  const [rows, setRows] = useState<ModelBreakdown[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setRows(null);
    setError(null);

    api
      .applicationModels(params.id, range)
      .then((data) => {
        if (!cancelled) setRows(data);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load model breakdown.");
      });

    return () => {
      cancelled = true;
    };
  }, [params.id, range, router]);

  const totalCost = rows?.reduce((sum, r) => sum + r.cost_micro, 0) ?? 0;

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-slate-700">Models & providers</h2>
        <RangePicker />
      </div>

      <ErrorBanner message={error} />

      {!rows && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {rows?.length === 0 && (
        <EmptyState>No traffic in this range yet.</EmptyState>
      )}

      {rows && rows.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                <th className="px-4 py-2 font-medium">Provider</th>
                <th className="px-4 py-2 font-medium">Model</th>
                <th className="px-4 py-2 font-medium text-right">Requests</th>
                <th className="px-4 py-2 font-medium text-right">Tokens</th>
                <th className="px-4 py-2 font-medium text-right">Errors</th>
                <th className="px-4 py-2 font-medium text-right">Avg latency</th>
                <th className="px-4 py-2 font-medium text-right">Spend</th>
                <th className="px-4 py-2 font-medium text-right">Share</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={`${r.provider}-${r.model}`} className="border-b border-slate-100 last:border-0">
                  <td className="px-4 py-2 text-slate-600">{r.provider}</td>
                  <td className="px-4 py-2 font-medium text-slate-900">{r.model}</td>
                  <td className="px-4 py-2 text-right">{formatNumber(r.requests)}</td>
                  <td className="px-4 py-2 text-right">{formatNumber(r.total_tokens)}</td>
                  <td className="px-4 py-2 text-right">{formatNumber(r.errors)}</td>
                  <td className="px-4 py-2 text-right">{formatDuration(r.avg_duration_ms)}</td>
                  <td className="px-4 py-2 text-right">{formatMoney(r.cost_micro)}</td>
                  <td className="px-4 py-2 text-right text-slate-500">
                    {formatPercent(totalCost > 0 ? r.cost_micro / totalCost : 0)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
