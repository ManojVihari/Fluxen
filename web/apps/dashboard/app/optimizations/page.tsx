"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, ApiError, type Opportunity } from "@/lib/api";
import { Badge, EmptyState, ErrorBanner } from "@/components/ui";
import { formatMoney, formatPercent } from "@/lib/format";

// The central opportunity feed (Part I.2). Phase 3 ships the minimal
// version — "enough to reach the detail page" — sorted by savings ×
// confidence like the API itself already returns it (Part G.2); the full
// filter/sort UX is a later-phase concern if time allows.
export default function OptimizationsPage() {
  const router = useRouter();
  const [opportunities, setOpportunities] = useState<Opportunity[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .listOpportunities()
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
  }, [router]);

  return (
    <main className="mx-auto max-w-3xl p-8">
      <div className="mb-6">
        <h1 className="text-xl font-semibold text-slate-900">Optimizations</h1>
        <p className="text-sm text-slate-500">
          What Fluxen found in your traffic. Estimated figures, never presented as measured.
        </p>
      </div>

      <ErrorBanner message={error} />

      {opportunities === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {opportunities?.length === 0 && (
        <EmptyState>No meaningful optimization found yet.</EmptyState>
      )}

      {opportunities && opportunities.length > 0 && (
        <div className="divide-y divide-slate-100 rounded-lg border border-slate-200 bg-white">
          {opportunities.map((o) => (
            <Link
              key={o.id}
              href={`/optimizations/${o.id}`}
              className="flex items-center justify-between gap-4 px-4 py-3 hover:bg-slate-50"
            >
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-slate-900">{o.title}</p>
                <p className="mt-0.5 text-xs text-slate-500">
                  {o.status} · est. {formatPercent(o.savings_pct)} savings
                </p>
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
    </main>
  );
}
