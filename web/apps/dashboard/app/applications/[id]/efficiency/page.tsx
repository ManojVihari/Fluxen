"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { api, ApiError, type Opportunity } from "@/lib/api";
import { Badge, EmptyState, ErrorBanner } from "@/components/ui";
import { formatMoney, formatPercent } from "@/lib/format";

// Application Detail's Efficiency tab — Phase 3 only wires the entry
// point into Optimizations ("at minimum an opportunity count"); the full
// efficiency score/ring is Part L Phase 7.
export default function ApplicationEfficiencyPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [opportunities, setOpportunities] = useState<Opportunity[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .listOpportunities({ appId: params.id, status: "open" })
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
  }, [params.id, router]);

  return (
    <div>
      <h2 className="mb-4 text-sm font-medium text-slate-700">Efficiency</h2>

      <ErrorBanner message={error} />

      {opportunities === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {opportunities?.length === 0 && (
        <EmptyState>
          No meaningful optimization found. Fluxen keeps watching this application's traffic.
        </EmptyState>
      )}

      {opportunities && opportunities.length > 0 && (
        <div className="space-y-2">
          <p className="text-sm text-slate-600">
            {opportunities.length} open {opportunities.length === 1 ? "opportunity" : "opportunities"} found.
          </p>
          {opportunities.map((o) => (
            <Link
              key={o.id}
              href={`/optimizations/${o.id}`}
              className="flex items-center justify-between gap-4 rounded-lg border border-slate-200 bg-white px-4 py-3 hover:bg-slate-50"
            >
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-slate-900">{o.title}</p>
                <p className="mt-0.5 text-xs text-slate-500">est. {formatPercent(o.savings_pct)} savings</p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Badge tone={o.confidence === "high" ? "good" : o.confidence === "low" ? "warn" : "neutral"}>
                  {o.confidence}
                </Badge>
                <span className="text-sm font-medium text-slate-900">
                  est. {formatMoney(o.savings_micro)}/mo
                </span>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
