"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  api,
  ApiError,
  type Application,
  type OverviewDailyPoint,
  type OverviewResponse,
} from "@/lib/api";
import { Badge, Button, EmptyState, ErrorBanner, StatTile } from "@/components/ui";
import { TopNav } from "@/components/top-nav";
import { RangePicker, useRangeParam } from "@/components/range-picker";
import { BarChart } from "@/components/bar-chart";
import { formatDay, formatMoney, formatNumber, formatPercent } from "@/lib/format";
import { Suspense } from "react";

// The root route decides where a visitor belongs (setup wizard, login,
// or — once authenticated — the Overview page itself, Part I.3: "the
// organization-wide entry point"). Before Phase 7 this route only ever
// redirected to /applications; now that an Overview API exists, "/" is
// the Overview screen for real.
export default function Home() {
  const router = useRouter();
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function route() {
      try {
        const status = await api.setupStatus();
        if (cancelled) return;
        if (!status.complete) {
          router.replace("/setup");
          return;
        }

        try {
          await api.session();
          if (!cancelled) setReady(true);
        } catch {
          if (!cancelled) router.replace("/login");
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof ApiError
              ? err.message
              : "Could not reach the Fluxen API. Is it running?"
          );
        }
      }
    }

    route();
    return () => {
      cancelled = true;
    };
  }, [router]);

  if (!ready) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-2 p-8">
        <h1 className="text-2xl font-semibold">Fluxen</h1>
        {error ? (
          <p className="max-w-sm text-center text-red-600">{error}</p>
        ) : (
          <p className="text-slate-500">Loading…</p>
        )}
      </main>
    );
  }

  return (
    <Suspense fallback={<p className="p-8 text-sm text-slate-500">Loading…</p>}>
      <OverviewContent />
    </Suspense>
  );
}

function OverviewContent() {
  const router = useRouter();
  const range = useRangeParam();

  const [apps, setApps] = useState<Application[] | null>(null);
  const [overview, setOverview] = useState<OverviewResponse | null>(null);
  const [timeseries, setTimeseries] = useState<OverviewDailyPoint[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .listApplications()
      .then((data) => {
        if (!cancelled) setApps(data);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [router]);

  useEffect(() => {
    let cancelled = false;
    setOverview(null);
    setTimeseries(null);
    setError(null);

    Promise.all([api.overview(range), api.overviewTimeseries(range)])
      .then(([o, ts]) => {
        if (cancelled) return;
        setOverview(o);
        setTimeseries(ts);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load the overview.");
      });

    return () => {
      cancelled = true;
    };
  }, [range, router]);

  // Empty state: zero applications shows the setup wizard entry point
  // instead of empty charts (Part I.3).
  if (apps !== null && apps.length === 0) {
    return (
      <>
        <TopNav />
        <main className="mx-auto max-w-3xl p-8">
          <EmptyState>
            <p className="mb-4">Welcome to Fluxen. Create your first application to get started.</p>
            <Link href="/applications/new">
              <Button>Create your first application</Button>
            </Link>
          </EmptyState>
        </main>
      </>
    );
  }

  return (
    <>
      <TopNav />
      <main className="mx-auto max-w-5xl p-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-slate-900">Overview</h1>
            <p className="text-sm text-slate-500">Every application, org-wide.</p>
          </div>
          <RangePicker />
        </div>

        <ErrorBanner message={error} />

        {!overview && !error && <p className="text-sm text-slate-500">Loading…</p>}

        {overview && (
          <div className="space-y-6">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <StatTile label="Spend" value={formatMoney(overview.cost_micro)} sub={`${range} range`} />
              <StatTile label="Requests" value={formatNumber(overview.requests)} />
              <StatTile
                label="Potential savings"
                value={formatMoney(overview.potential_savings_micro)}
                sub="est. — open opportunities"
              />
              <StatTile
                label="Realized savings"
                value={formatMoney(overview.realized_savings_micro)}
                sub="measured — final verdicts"
              />
            </div>

            {timeseries && timeseries.length > 0 && (
              <div className="rounded-lg border border-slate-200 bg-white p-4">
                <p className="mb-3 text-xs font-medium uppercase tracking-wide text-slate-500">
                  Cost over time
                </p>
                <BarChart
                  data={timeseries.map((p) => ({ label: formatDay(p.day), value: p.cost_micro }))}
                  valueLabel={formatMoney}
                />
              </div>
            )}

            {overview.provider_mix.length > 0 && (
              <div className="rounded-lg border border-slate-200 bg-white p-4">
                <p className="mb-3 text-xs font-medium uppercase tracking-wide text-slate-500">
                  Provider mix
                </p>
                <ProviderMixBar mix={overview.provider_mix} />
              </div>
            )}

            <div className="rounded-lg border border-slate-200 bg-white">
              <p className="border-b border-slate-100 px-4 py-3 text-xs font-medium uppercase tracking-wide text-slate-500">
                Top applications
              </p>
              {overview.top_applications.length === 0 ? (
                <div className="px-4 py-6">
                  <EmptyState>No traffic in this range yet.</EmptyState>
                </div>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-100 text-left text-xs uppercase tracking-wide text-slate-500">
                      <th className="px-4 py-2 font-medium">Application</th>
                      <th className="px-4 py-2 font-medium text-right">Spend</th>
                      <th className="px-4 py-2 font-medium text-right">Δ vs. prior</th>
                      <th className="px-4 py-2 font-medium text-right">Efficiency</th>
                      <th className="px-4 py-2 font-medium text-right">Opportunity value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {overview.top_applications.map((a) => (
                      <tr key={a.app_id} className="border-b border-slate-100 last:border-0">
                        <td className="px-4 py-3">
                          <Link
                            href={`/applications/${a.app_id}`}
                            className="font-medium text-slate-900 hover:underline"
                          >
                            {a.name}
                          </Link>
                        </td>
                        <td className="px-4 py-3 text-right">{formatMoney(a.cost_micro)}</td>
                        <td className="px-4 py-3 text-right">
                          <DeltaLabel current={a.cost_micro} prior={a.prior_cost_micro} />
                        </td>
                        <td className="px-4 py-3 text-right">
                          {a.efficiency_score != null ? `${a.efficiency_score}/100` : "—"}
                        </td>
                        <td className="px-4 py-3 text-right">
                          {a.open_opportunity_value_micro > 0
                            ? formatMoney(a.open_opportunity_value_micro)
                            : "—"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>

            <div className="rounded-lg border border-slate-200 bg-white">
              <p className="border-b border-slate-100 px-4 py-3 text-xs font-medium uppercase tracking-wide text-slate-500">
                Opportunities
              </p>
              {overview.opportunity_feed.length === 0 ? (
                <div className="px-4 py-6">
                  <EmptyState>
                    Opportunities will appear once your applications have enough traffic.
                  </EmptyState>
                </div>
              ) : (
                <div className="divide-y divide-slate-100">
                  {overview.opportunity_feed.map((o) => (
                    <div key={o.id} className="flex items-center justify-between gap-4 px-4 py-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-slate-900">{o.title}</p>
                        <p className="mt-0.5 text-xs text-slate-500">
                          est. {formatPercent(o.savings_pct)} savings
                        </p>
                      </div>
                      <div className="flex shrink-0 items-center gap-3">
                        <Badge tone={o.confidence === "high" ? "good" : o.confidence === "low" ? "warn" : "neutral"}>
                          {o.confidence}
                        </Badge>
                        <span className="text-sm font-medium text-slate-900">
                          est. {formatMoney(o.savings_micro)}/mo
                        </span>
                        <Link href={`/optimizations/${o.id}`}>
                          <Button variant="secondary">Review</Button>
                        </Link>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </main>
    </>
  );
}

function DeltaLabel({ current, prior }: { current: number; prior: number }) {
  if (prior === 0) return <span className="text-slate-400">—</span>;
  const change = (current - prior) / prior;
  const tone = change > 0.05 ? "text-red-600" : change < -0.05 ? "text-emerald-600" : "text-slate-500";
  const sign = change > 0 ? "+" : "";
  return <span className={tone}>{sign}{formatPercent(change)}</span>;
}

function ProviderMixBar({ mix }: { mix: { provider: string; cost_micro: number }[] }) {
  const total = mix.reduce((sum, m) => sum + m.cost_micro, 0) || 1;
  const colors = ["bg-slate-700", "bg-emerald-500", "bg-amber-500", "bg-sky-500"];
  return (
    <div>
      <div className="flex h-3 w-full overflow-hidden rounded-full bg-slate-100">
        {mix.map((m, i) => (
          <div
            key={m.provider}
            className={colors[i % colors.length]}
            style={{ width: `${(m.cost_micro / total) * 100}%` }}
            title={`${m.provider}: ${formatPercent(m.cost_micro / total)}`}
          />
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-600">
        {mix.map((m, i) => (
          <span key={m.provider} className="inline-flex items-center gap-1.5">
            <span className={`h-2 w-2 rounded-full ${colors[i % colors.length]}`} />
            {m.provider} · {formatPercent(m.cost_micro / total)}
          </span>
        ))}
      </div>
    </div>
  );
}
