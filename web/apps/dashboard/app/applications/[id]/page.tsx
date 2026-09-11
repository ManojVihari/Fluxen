"use client";

import { Suspense, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, ApiError, type ApplicationSummary, type DailyPoint } from "@/lib/api";
import { EmptyState, ErrorBanner, StatTile } from "@/components/ui";
import { RangePicker, useRangeParam } from "@/components/range-picker";
import { BarChart } from "@/components/bar-chart";
import { formatDay, formatDuration, formatMoney, formatNumber, formatPercent } from "@/lib/format";

// Application Detail's Usage & Cost tab — Phase 2's core deliverable:
// "requests, tokens, cost, model/provider mix, latency, and errors, for a
// real (or seeded) application" (Part L Phase 2 exit criteria). Model mix
// itself lives on the Models tab; this tab is the summary + trend.
export default function UsageAndCostPage() {
  return (
    <Suspense fallback={<p className="text-sm text-slate-500">Loading…</p>}>
      <UsageAndCostContent />
    </Suspense>
  );
}

function UsageAndCostContent() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const range = useRangeParam();

  const [summary, setSummary] = useState<ApplicationSummary | null>(null);
  const [points, setPoints] = useState<DailyPoint[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notEnoughData, setNotEnoughData] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setSummary(null);
    setPoints(null);
    setError(null);
    setNotEnoughData(false);

    Promise.all([
      api.applicationSummary(params.id, range),
      api.applicationTimeseries(params.id, range),
    ])
      .then(([s, p]) => {
        if (cancelled) return;
        setSummary(s);
        setPoints(p);
        setNotEnoughData(s.requests === 0);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load usage data.");
      });

    return () => {
      cancelled = true;
    };
  }, [params.id, range, router]);

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-slate-700">Usage & Cost</h2>
        <RangePicker />
      </div>

      <ErrorBanner message={error} />

      {!summary && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {notEnoughData && summary && (
        <EmptyState>
          No traffic in this range yet. Point an application at this API key
          and traffic will show up here within seconds.
        </EmptyState>
      )}

      {summary && !notEnoughData && (
        <>
          <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <StatTile label="Spend" value={formatMoney(summary.cost_micro)} />
            <StatTile label="Requests" value={formatNumber(summary.requests)} />
            <StatTile
              label="Tokens"
              value={formatNumber(summary.total_tokens)}
              sub={`${formatNumber(summary.input_tokens)} in / ${formatNumber(summary.output_tokens)} out`}
            />
            <StatTile
              label="Errors"
              value={formatPercent(summary.requests > 0 ? summary.errors / summary.requests : 0)}
              sub={`${formatNumber(summary.errors)} of ${formatNumber(summary.requests)}`}
            />
          </div>

          <div className="mb-6 rounded-lg border border-slate-200 bg-white p-4">
            <p className="mb-1 text-xs font-medium uppercase tracking-wide text-slate-500">
              Avg latency
            </p>
            <p className="text-lg font-semibold text-slate-900">
              {formatDuration(summary.avg_duration_ms)}
            </p>
          </div>

          {points && points.length > 0 && (
            <div className="rounded-lg border border-slate-200 bg-white p-4">
              <p className="mb-3 text-xs font-medium uppercase tracking-wide text-slate-500">
                Daily spend
              </p>
              <BarChart
                data={points.map((p) => ({ label: formatDay(p.day), value: p.cost_micro }))}
                valueLabel={formatMoney}
              />
            </div>
          )}
        </>
      )}
    </div>
  );
}
