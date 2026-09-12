"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  api,
  ApiError,
  type ModelCostEvidence,
  type ModelCostRecommendation,
  type ModelMixBreakdownRow,
  type Opportunity,
  type Simulation,
} from "@/lib/api";
import { Badge, Button, ErrorBanner } from "@/components/ui";
import { formatMoney, formatNumber, formatPercent } from "@/lib/format";

// The Optimizations detail page — Part I.2's core Fluxen UX, strictly
// ordered Why -> Evidence -> Impact -> Simulate -> Apply -> Measure.
// Phase 3 built the first three sections against real detector output;
// Phase 4 makes Simulate fully functional for the model-cost kind
// (Part L Phase 4 frontend tasks); Apply/Measure stay a disabled
// preview of Phase 5/6.
export default function OpportunityDetailPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [opportunity, setOpportunity] = useState<Opportunity | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    api
      .getOpportunity(params.id)
      .then((o) => {
        if (cancelled) return;
        setOpportunity(o);
        // Auto-transition open -> reviewed on first view (Part G.1).
        if (o.status === "open") {
          api.reviewOpportunity(params.id).then((reviewed) => {
            if (!cancelled) setOpportunity(reviewed);
          }).catch(() => {
            // Non-critical — the view still renders with the un-reviewed
            // status if this fails.
          });
        }
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        if (err instanceof ApiError && err.status === 404) {
          setError("Opportunity not found.");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load opportunity.");
      });

    return () => {
      cancelled = true;
    };
  }, [params.id, router]);

  return (
    <main className="mx-auto max-w-3xl p-8">
      <Link href="/optimizations" className="text-sm text-slate-500 underline-offset-2 hover:underline">
        ← Optimizations
      </Link>

      <ErrorBanner message={error} />

      {!opportunity && !error && <p className="mt-4 text-sm text-slate-500">Loading…</p>}

      {opportunity && (
        <div className="mt-4 space-y-6">
          <Header opportunity={opportunity} />
          <WhySection opportunity={opportunity} />
          <EvidenceSection opportunity={opportunity} />
          <ImpactSection opportunity={opportunity} />
          <SimulateSection opportunity={opportunity} />
        </div>
      )}
    </main>
  );
}

function Header({ opportunity }: { opportunity: Opportunity }) {
  const confidenceTooltip =
    opportunity.confidence === "high"
      ? "High confidence: a strong, consistent share of traffic fits the candidate model, backed by a large sample."
      : opportunity.confidence === "medium"
        ? "Medium confidence: a solid share of traffic fits, but the sample size or cross-provider pairing limits certainty."
        : "Low confidence: the pattern is real but based on a smaller or less consistent sample.";

  return (
    <div className="flex items-start justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold text-slate-900">{opportunity.title}</h1>
        <p className="mt-1 text-xs uppercase tracking-wide text-slate-400">
          {opportunity.status} · {opportunity.kind.replace("_", " ")}
        </p>
      </div>
      <Badge
        tone={opportunity.confidence === "high" ? "good" : opportunity.confidence === "low" ? "warn" : "neutral"}
        title={confidenceTooltip}
      >
        {opportunity.confidence} confidence
      </Badge>
    </div>
  );
}

function WhySection({ opportunity }: { opportunity: Opportunity }) {
  return (
    <Section title="Why">
      <p className="text-sm text-slate-700">{opportunity.summary}</p>
    </Section>
  );
}

function EvidenceSection({ opportunity }: { opportunity: Opportunity }) {
  if (opportunity.kind !== "model_cost") {
    return (
      <Section title="Evidence">
        <p className="text-sm text-slate-500">No evidence panel for this opportunity kind yet.</p>
      </Section>
    );
  }

  const evidence = opportunity.evidence as ModelCostEvidence;
  const recommendation = opportunity.recommendation as ModelCostRecommendation;

  return (
    <Section title="Evidence">
      <div className="space-y-4">
        <div className="flex items-center gap-3 text-sm">
          <span className="rounded-md bg-slate-100 px-2 py-1 font-medium text-slate-700">
            {evidence.current_model}
          </span>
          <span className="text-slate-400">→</span>
          <span className="rounded-md bg-emerald-50 px-2 py-1 font-medium text-emerald-700">
            {evidence.candidate_model}
          </span>
          <span className="text-xs text-slate-400">
            (list price ratio ~{(evidence.price_ratio * 100).toFixed(0)}% of current)
          </span>
        </div>

        <div>
          <div className="mb-1 flex justify-between text-xs text-slate-500">
            <span>Eligible traffic</span>
            <span>
              {formatNumber(evidence.eligible_requests)} / {formatNumber(evidence.total_requests)} (
              {formatPercent(evidence.eligible_fraction)})
            </span>
          </div>
          <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100">
            <div
              className="h-full rounded-full bg-emerald-500"
              style={{ width: `${Math.min(evidence.eligible_fraction * 100, 100)}%` }}
            />
          </div>
        </div>

        {Object.keys(evidence.exclusion_breakdown).length > 0 && (
          <div>
            <p className="mb-1 text-xs font-medium uppercase tracking-wide text-slate-500">
              Why the rest were excluded
            </p>
            <ul className="space-y-1 text-sm text-slate-600">
              {Object.entries(evidence.exclusion_breakdown).map(([reason, count]) => (
                <li key={reason} className="flex justify-between">
                  <span>{reason.replace(/_/g, " ")}</span>
                  <span>{formatNumber(count)}</span>
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
          <TokenStat label="Input p50" value={evidence.input_tokens_p50} />
          <TokenStat label="Input p95" value={evidence.input_tokens_p95} />
          <TokenStat label="Output p50" value={evidence.output_tokens_p50} />
          <TokenStat label="Output p95" value={evidence.output_tokens_p95} />
        </div>

        <p className="text-xs text-slate-500">
          Recommended: shift {formatPercent(recommendation.recommended_traffic_weight)} of traffic to{" "}
          {recommendation.candidate_model} (a conservative split, never a full replacement).
        </p>

        <p className="rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-800">{evidence.caveat}</p>
      </div>
    </Section>
  );
}

function TokenStat({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <p className="text-xs text-slate-400">{label}</p>
      <p className="font-medium text-slate-900">{formatNumber(value)}</p>
    </div>
  );
}

function ImpactSection({ opportunity }: { opportunity: Opportunity }) {
  return (
    <Section title="Impact">
      <div className="grid grid-cols-3 gap-4 text-sm">
        <Impact label="Current cost" value={formatMoney(opportunity.current_cost_micro)} />
        <Impact label="Projected cost" value={formatMoney(opportunity.projected_cost_micro)} />
        <Impact
          label="Potential savings"
          value={`${formatMoney(opportunity.savings_micro)} (${formatPercent(opportunity.savings_pct)})`}
        />
      </div>
      <p className="mt-3 text-xs text-slate-400">
        All figures are est. — projected from the last {"14"} days of real traffic, normalized to a
        30-day month. Not a measured result.
      </p>
    </Section>
  );
}

function Impact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-slate-500">est. {label}</p>
      <p className="mt-0.5 text-base font-semibold text-slate-900">{value}</p>
    </div>
  );
}

function SimulateSection({ opportunity }: { opportunity: Opportunity }) {
  if (opportunity.kind !== "model_cost") {
    return (
      <Section title="Simulate">
        <p className="text-sm text-slate-500">No simulation scenario for this opportunity kind yet.</p>
      </Section>
    );
  }

  return <ModelMixSimulateSection opportunity={opportunity} />;
}

function ModelMixSimulateSection({ opportunity }: { opportunity: Opportunity }) {
  const recommendation = opportunity.recommendation as ModelCostRecommendation;

  // Pre-filled from the opportunity's own recommendation (Part I.2:
  // "scenario form pre-filled from recommendation").
  const [weight, setWeight] = useState(recommendation.recommended_traffic_weight);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<Simulation | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function runSimulation() {
    setRunning(true);
    setError(null);
    try {
      const sim = await api.createSimulation({
        app_id: opportunity.app_id,
        opportunity_id: opportunity.id,
        scenario: {
          type: "model_mix",
          current_model: recommendation.current_model,
          candidate_model: recommendation.candidate_model,
          traffic_weight: weight,
        },
      });
      setResult(sim);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to run simulation.");
    } finally {
      setRunning(false);
    }
  }

  const breakdown = (result?.breakdown as ModelMixBreakdownRow[] | undefined) ?? [];
  const assumptions = (result?.assumptions as string[] | undefined) ?? [];

  return (
    <Section title="Simulate">
      <div className="space-y-4">
        <div>
          <label className="mb-1 block text-xs font-medium text-slate-600">
            Traffic to reroute from {recommendation.current_model} to {recommendation.candidate_model}
          </label>
          <div className="flex items-center gap-3">
            <input
              type="range"
              min={0.05}
              max={1}
              step={0.05}
              value={weight}
              onChange={(e) => setWeight(Number(e.target.value))}
              className="w-full"
            />
            <span className="w-14 shrink-0 text-right text-sm font-medium text-slate-900">
              {formatPercent(weight)}
            </span>
          </div>
        </div>

        <ErrorBanner message={error} />

        <Button onClick={runSimulation} disabled={running}>
          {running ? "Running…" : "Run simulation"}
        </Button>

        {result && (
          <div className="rounded-md border border-slate-200 p-3">
            <p className="mb-2 text-xs font-medium uppercase tracking-wide text-slate-500">
              Replayed {formatNumber(result.replayed_requests)} real requests
              {result.sampled ? " (sampled)" : ""} over {result.window_start.slice(0, 10)} –{" "}
              {result.window_end.slice(0, 10)}
            </p>

            <div className="grid grid-cols-3 gap-4 text-sm">
              <Impact label="Current cost" value={formatMoney(result.actual_cost_micro)} />
              <Impact label="Simulated cost" value={formatMoney(result.simulated_cost_micro)} />
              <Impact
                label="Delta"
                value={`${formatMoney(result.delta_micro)} (${formatPercent(result.delta_pct)})`}
              />
            </div>

            <p className="mt-2 text-xs text-slate-500">
              {formatNumber(result.affected_requests)} of {formatNumber(result.replayed_requests)} requests
              would have been rerouted.
            </p>

            {breakdown.length > 0 && (
              <table className="mt-3 w-full text-xs">
                <thead>
                  <tr className="border-b border-slate-200 text-left text-slate-500">
                    <th className="py-1 font-medium">Model</th>
                    <th className="py-1 text-right font-medium">Requests</th>
                    <th className="py-1 text-right font-medium">Actual</th>
                    <th className="py-1 text-right font-medium">Simulated</th>
                  </tr>
                </thead>
                <tbody>
                  {breakdown.map((row) => (
                    <tr key={row.model} className="border-b border-slate-100 last:border-0">
                      <td className="py-1 text-slate-700">{row.model}</td>
                      <td className="py-1 text-right">{formatNumber(row.requests)}</td>
                      <td className="py-1 text-right">{formatMoney(row.actual_cost_micro)}</td>
                      <td className="py-1 text-right">{formatMoney(row.simulated_cost_micro)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}

            {assumptions.length > 0 && (
              <ul className="mt-3 space-y-1 text-xs text-slate-500">
                {assumptions.map((a) => (
                  <li key={a}>⚠ {a}</li>
                ))}
              </ul>
            )}
          </div>
        )}

        <div className="flex gap-2 border-t border-slate-100 pt-3">
          <button
            disabled
            title={result ? "Apply arrives in the next phase" : "Run a simulation first"}
            className="cursor-not-allowed rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-400"
          >
            Apply
          </button>
        </div>
      </div>
    </Section>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border border-slate-200 bg-white p-4">
      <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-500">{title}</h2>
      {children}
    </section>
  );
}
