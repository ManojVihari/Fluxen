"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { api, ApiError, type Opportunity } from "@/lib/api";
import { Badge, EmptyState, ErrorBanner } from "@/components/ui";
import { formatMoney, formatPercent } from "@/lib/format";

const STATUS_OPTIONS = ["open", "reviewed", "simulated", "applied", "dismissed", "stale", "reverted"];
const selectClass =
  "rounded-md border border-slate-300 bg-white px-2.5 py-1.5 text-sm text-slate-700 focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500";

// Application Detail's Opportunities tab (Part I.1) — this application's
// own opportunity list across all four detector kinds, distinct from the
// Efficiency tab (the score breakdown) right next to it.
export default function ApplicationOpportunitiesPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [opportunities, setOpportunities] = useState<Opportunity[] | null>(null);
  const [statusFilter, setStatusFilter] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setOpportunities(null);
    setError(null);
    api
      .listOpportunities({ appId: params.id, status: statusFilter || undefined })
      .then((data) => {
        if (!cancelled) setOpportunities(data);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load opportunities.");
      });
    return () => {
      cancelled = true;
    };
  }, [params.id, statusFilter, router]);

  const sorted = useMemo(() => {
    if (!opportunities) return null;
    return [...opportunities].sort(
      (a, b) => b.savings_micro * b.confidence_score - a.savings_micro * a.confidence_score
    );
  }, [opportunities]);

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-slate-700">Opportunities</h2>
        <select className={selectClass} value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
          <option value="">All statuses</option>
          {STATUS_OPTIONS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>

      <ErrorBanner message={error} />

      {sorted === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {sorted?.length === 0 && (
        <EmptyState>
          {statusFilter
            ? "No opportunities match this filter."
            : "No meaningful optimization found — check back as traffic grows."}
        </EmptyState>
      )}

      {sorted && sorted.length > 0 && (
        <div className="divide-y divide-slate-100 rounded-lg border border-slate-200 bg-white">
          {sorted.map((o) => (
            <Link
              key={o.id}
              href={`/optimizations/${o.id}`}
              className="flex items-center justify-between gap-4 px-4 py-3 hover:bg-slate-50"
            >
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-slate-900">{o.title}</p>
                <p className="mt-0.5 text-xs text-slate-500">
                  {o.status} · {o.kind.replace(/_/g, " ")}
                  {o.kind !== "traffic_anomaly" && <> · est. {formatPercent(o.savings_pct)} savings</>}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Badge tone={o.confidence === "high" ? "good" : o.confidence === "low" ? "warn" : "neutral"}>
                  {o.confidence}
                </Badge>
                {o.kind === "traffic_anomaly" ? (
                  o.severity && <Badge tone={o.severity === "high" ? "warn" : "neutral"}>{o.severity}</Badge>
                ) : (
                  <span className="text-sm font-medium text-slate-900">
                    est. {formatMoney(o.savings_micro)}/mo
                  </span>
                )}
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
