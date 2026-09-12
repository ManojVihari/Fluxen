"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, ApiError, type Application, type Opportunity } from "@/lib/api";
import { Badge, EmptyState, ErrorBanner } from "@/components/ui";
import { TopNav } from "@/components/top-nav";
import { formatMoney, formatPercent } from "@/lib/format";

const STATUS_OPTIONS = ["open", "reviewed", "simulated", "applied", "dismissed", "stale", "reverted"];
const KIND_OPTIONS = ["model_cost", "repeated_request", "token_efficiency", "traffic_anomaly"];

const selectClass =
  "rounded-md border border-slate-300 bg-white px-2.5 py-1.5 text-sm text-slate-700 focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500";

// The central opportunity feed (Part I.2): status/kind/application
// filters, default sort savings_micro x confidence_score descending
// (Part G.2's own ranking rule, reapplied here client-side since the
// list endpoint itself orders by detection time — kind has no backend
// filter either, so both are applied in the browser over the already-
// small, per-org-capped result set).
export default function OptimizationsPage() {
  const router = useRouter();
  const [opportunities, setOpportunities] = useState<Opportunity[] | null>(null);
  const [apps, setApps] = useState<Application[]>([]);
  const [error, setError] = useState<string | null>(null);

  const [statusFilter, setStatusFilter] = useState("open");
  const [kindFilter, setKindFilter] = useState("");
  const [appFilter, setAppFilter] = useState("");

  useEffect(() => {
    api.listApplications().then(setApps).catch(() => {});
  }, []);

  useEffect(() => {
    let cancelled = false;
    setOpportunities(null);
    setError(null);
    api
      .listOpportunities({ status: statusFilter || undefined, appId: appFilter || undefined })
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
  }, [statusFilter, appFilter, router]);

  const appNameById = useMemo(() => new Map(apps.map((a) => [a.id, a.name])), [apps]);

  const filtered = useMemo(() => {
    if (!opportunities) return null;
    const rows = kindFilter ? opportunities.filter((o) => o.kind === kindFilter) : opportunities;
    return [...rows].sort((a, b) => b.savings_micro * b.confidence_score - a.savings_micro * a.confidence_score);
  }, [opportunities, kindFilter]);

  return (
    <>
      <TopNav />
      <main className="mx-auto max-w-3xl p-8">
        <div className="mb-6">
          <h1 className="text-xl font-semibold text-slate-900">Optimizations</h1>
          <p className="text-sm text-slate-500">
            What Fluxen found in your traffic. Estimated figures, never presented as measured.
          </p>
        </div>

        <div className="mb-4 flex flex-wrap gap-2">
          <select className={selectClass} value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
            <option value="">All statuses</option>
            {STATUS_OPTIONS.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <select className={selectClass} value={kindFilter} onChange={(e) => setKindFilter(e.target.value)}>
            <option value="">All kinds</option>
            {KIND_OPTIONS.map((k) => (
              <option key={k} value={k}>
                {k.replace(/_/g, " ")}
              </option>
            ))}
          </select>
          <select className={selectClass} value={appFilter} onChange={(e) => setAppFilter(e.target.value)}>
            <option value="">All applications</option>
            {apps.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </div>

        <ErrorBanner message={error} />

        {filtered === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

        {filtered?.length === 0 && <EmptyState>No opportunities match these filters.</EmptyState>}

        {filtered && filtered.length > 0 && (
          <div className="divide-y divide-slate-100 rounded-lg border border-slate-200 bg-white">
            {filtered.map((o) => (
              <Link
                key={o.id}
                href={`/optimizations/${o.id}`}
                className="flex items-center justify-between gap-4 px-4 py-3 hover:bg-slate-50"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-slate-900">{o.title}</p>
                  <p className="mt-0.5 text-xs text-slate-500">
                    {appNameById.get(o.app_id) ?? o.app_id} · {o.status} · {o.kind.replace(/_/g, " ")}
                    {o.kind !== "traffic_anomaly" && <> · est. {formatPercent(o.savings_pct)} savings</>}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Badge tone={o.confidence === "high" ? "good" : o.confidence === "low" ? "warn" : "neutral"}>
                    {o.confidence}
                  </Badge>
                  {o.kind === "traffic_anomaly" ? (
                    o.severity && (
                      <Badge tone={o.severity === "high" ? "warn" : "neutral"}>{o.severity}</Badge>
                    )
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
      </main>
    </>
  );
}
