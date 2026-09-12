"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  api,
  ApiError,
  type Measurement,
  type ModelCostEvidence,
  type ModelCostRecommendation,
  type ModelMixBreakdownRow,
  type Opportunity,
  type RepeatedRequestEvidence,
  type RepeatedRequestRecommendation,
  type Simulation,
  type TokenEfficiencyEvidence,
  type TrafficAnomalyEvidence,
} from "@/lib/api";
import { Badge, Button, ErrorBanner } from "@/components/ui";
import { BarChart } from "@/components/bar-chart";
import { formatMoney, formatNumber, formatPercent } from "@/lib/format";
import { confidenceStyle, severityStyle, statusStyle } from "@/lib/opportunity-status";

// The Optimizations detail page — Part I.2's core Fluxen UX, strictly
// ordered Why -> Evidence -> Impact -> Simulate -> Apply -> Measure.
// Phase 3 built the first three sections against real detector output;
// Phase 4 made Simulate fully functional; Phase 5 made Apply fully
// functional; Phase 6 makes Measure fully functional — the honest
// before/after verdict on whether an applied change actually worked.
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
          <ReviewDismissActions opportunity={opportunity} onChange={setOpportunity} />
          <WhySection opportunity={opportunity} />
          <EvidenceSection opportunity={opportunity} />
          <ImpactSection opportunity={opportunity} />
          <SimulateSection opportunity={opportunity} />
          <MeasureSection opportunity={opportunity} />
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
        <div className="flex items-center gap-2">
          <h1 className="text-xl font-semibold text-slate-900">{opportunity.title}</h1>
          <Badge tone={statusStyle(opportunity.status).tone}>{statusStyle(opportunity.status).label}</Badge>
        </div>
        <p className="mt-1 text-xs uppercase tracking-wide text-slate-400">
          {opportunity.kind.replace("_", " ")}
        </p>
      </div>
      <Badge tone={confidenceStyle(opportunity.confidence).tone} title={confidenceTooltip}>
        {opportunity.confidence} confidence
      </Badge>
    </div>
  );
}

// ReviewDismissActions replaces the old "auto-marks reviewed just from
// opening the page" behavior with two explicit choices: acknowledge it
// (Mark reviewed) or set it aside (Dismiss — "not now," with an optional
// reason, never a delete). Only shown while the opportunity is still in
// an actionable state; once applied/dismissed/reverted, the outcome
// itself is the record and these actions no longer apply.
function ReviewDismissActions({
  opportunity,
  onChange,
}: {
  opportunity: Opportunity;
  onChange: (o: Opportunity) => void;
}) {
  const [dismissing, setDismissing] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!["open", "reviewed", "simulated"].includes(opportunity.status)) {
    return null;
  }

  async function markReviewed() {
    setBusy(true);
    setError(null);
    try {
      onChange(await api.reviewOpportunity(opportunity.id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to mark as reviewed.");
    } finally {
      setBusy(false);
    }
  }

  async function confirmDismiss() {
    setBusy(true);
    setError(null);
    try {
      onChange(await api.dismissOpportunity(opportunity.id, reason || undefined));
      setDismissing(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to dismiss.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <ErrorBanner message={error} />
      {!dismissing ? (
        <div className="flex flex-wrap items-center gap-2">
          {opportunity.status === "open" && (
            <Button variant="secondary" onClick={markReviewed} disabled={busy}>
              Mark reviewed
            </Button>
          )}
          <Button variant="secondary" onClick={() => setDismissing(true)} disabled={busy}>
            Dismiss (maybe later)
          </Button>
        </div>
      ) : (
        <div className="space-y-3">
          <p className="text-sm font-medium text-slate-900">Dismiss this opportunity?</p>
          <p className="text-xs text-slate-500">
            It leaves the open feed but isn&apos;t deleted — if Fluxen sees the same pattern again later,
            it can re-open.
          </p>
          <input
            className="w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="Reason (optional)"
          />
          <div className="flex gap-2">
            <Button onClick={confirmDismiss} disabled={busy}>
              {busy ? "Dismissing…" : "Confirm dismiss"}
            </Button>
            <Button variant="secondary" onClick={() => { setDismissing(false); setReason(""); setError(null); }}>
              Cancel
            </Button>
          </div>
        </div>
      )}
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
  if (opportunity.kind === "repeated_request") {
    return <RepeatedRequestEvidenceSection opportunity={opportunity} />;
  }
  if (opportunity.kind === "token_efficiency") {
    return <TokenEfficiencyEvidenceSection opportunity={opportunity} />;
  }
  if (opportunity.kind === "traffic_anomaly") {
    return <TrafficAnomalyEvidenceSection opportunity={opportunity} />;
  }
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

// RepeatedRequestEvidenceSection renders Part G.3.2's own evidence: the
// TTL sweep table so the user can see the savings curve across all four
// candidate TTLs (not just the one Fluxen recommends), plus the top
// duplicated request shapes — grouped by system_prompt_hash + model,
// never raw prompt text.
function RepeatedRequestEvidenceSection({ opportunity }: { opportunity: Opportunity }) {
  const evidence = opportunity.evidence as RepeatedRequestEvidence;
  const recommendation = opportunity.recommendation as RepeatedRequestRecommendation;

  return (
    <Section title="Evidence">
      <div className="space-y-4">
        <div>
          <div className="mb-1 flex justify-between text-xs text-slate-500">
            <span>Exact-duplicate rate (last {evidence.window_days}d)</span>
            <span>
              {formatNumber(evidence.duplicate_requests)} / {formatNumber(evidence.total_requests)} (
              {formatPercent(evidence.duplicate_rate)})
            </span>
          </div>
          <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100">
            <div
              className="h-full rounded-full bg-emerald-500"
              style={{ width: `${Math.min(evidence.duplicate_rate * 100, 100)}%` }}
            />
          </div>
        </div>

        <div>
          <p className="mb-1 text-xs font-medium uppercase tracking-wide text-slate-500">TTL sweep</p>
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-slate-200 text-left text-slate-500">
                <th className="py-1 font-medium">TTL</th>
                <th className="py-1 text-right font-medium">Duplicate hits</th>
                <th className="py-1 text-right font-medium">est. savings/mo</th>
              </tr>
            </thead>
            <tbody>
              {evidence.ttl_sweep.map((row) => (
                <tr key={row.ttl} className="border-b border-slate-100 last:border-0">
                  <td className="py-1 text-slate-700">
                    {row.ttl}
                    {recommendation.recommended_ttl_seconds === ttlToSeconds(row.ttl) && (
                      <span className="ml-2 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] text-emerald-700">
                        recommended
                      </span>
                    )}
                  </td>
                  <td className="py-1 text-right">{formatNumber(row.duplicate_hits)}</td>
                  <td className="py-1 text-right">{formatMoney(row.savings_micro)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {evidence.top_shapes.length > 0 && (
          <div>
            <p className="mb-1 text-xs font-medium uppercase tracking-wide text-slate-500">
              Top duplicated request shapes
            </p>
            <ul className="space-y-1 text-sm text-slate-600">
              {evidence.top_shapes.map((shape) => (
                <li key={shape.system_prompt_hash + shape.model} className="flex justify-between">
                  <span className="truncate">
                    {shape.model} · prompt {shape.system_prompt_hash.slice(0, 8)}… · ~
                    {formatNumber(shape.input_tokens_p50)} input tokens
                  </span>
                  <span>{formatNumber(shape.count)}×</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </Section>
  );
}

function ttlToSeconds(label: string): number {
  const map: Record<string, number> = { "5m": 300, "1h": 3600, "6h": 21600, "24h": 86400 };
  return map[label] ?? -1;
}

// TokenEfficiencyEvidenceSection renders Part G.3.3's evidence: the
// baseline-vs-current mean-token drift plus the daily sparkline (with
// the change-point date, when one was found) so the user can see when
// the drift started, not just that it exists.
function TokenEfficiencyEvidenceSection({ opportunity }: { opportunity: Opportunity }) {
  const evidence = opportunity.evidence as TokenEfficiencyEvidence;

  return (
    <Section title="Evidence">
      <div className="space-y-4">
        <div className="flex items-center gap-3 text-sm">
          <Badge>{evidence.dimension} tokens</Badge>
          <span className="text-slate-700">{evidence.model}</span>
        </div>

        <div className="grid grid-cols-3 gap-4 text-sm">
          <TokenStat label="Baseline mean" value={Math.round(evidence.baseline_mean_tokens)} />
          <TokenStat label="Current mean" value={Math.round(evidence.current_mean_tokens)} />
          <div>
            <p className="text-xs text-slate-400">Drift</p>
            <p className="font-medium text-red-600">+{formatPercent(evidence.drift_pct)}</p>
          </div>
        </div>

        {evidence.daily_sparkline.length > 0 && (
          <div>
            <p className="mb-1 text-xs font-medium uppercase tracking-wide text-slate-500">
              Daily mean, trailing window
              {evidence.change_point_date && ` · drift began around ${evidence.change_point_date}`}
            </p>
            <BarChart
              data={evidence.daily_sparkline.map((p) => ({ label: p.day.slice(5), value: p.mean_tokens }))}
              valueLabel={(v) => `${Math.round(v)} tok`}
            />
          </div>
        )}

        <p className="text-xs text-slate-500">
          Based on {formatNumber(evidence.baseline_requests)} baseline and{" "}
          {formatNumber(evidence.current_requests)} current-window requests for {evidence.model}.
        </p>
      </div>
    </Section>
  );
}

// TrafficAnomalyEvidenceSection renders Part G.3.4's evidence: the
// hourly series around the anomalous window and how far it deviated
// from this application's own seasonal baseline. Traffic anomaly
// candidates carry no cost/savings figures at all (Part G.3.4: "no
// savings number is attached to an anomaly") — this is the one kind
// whose evidence is the entire story, not a supplement to an Impact
// section.
function TrafficAnomalyEvidenceSection({ opportunity }: { opportunity: Opportunity }) {
  const evidence = opportunity.evidence as TrafficAnomalyEvidence;

  return (
    <Section title="Evidence">
      <div className="space-y-4">
        <div className="flex items-center gap-3 text-sm">
          <Badge tone={severityStyle(opportunity.severity ?? "medium").tone}>
            {opportunity.severity ?? "medium"} severity
          </Badge>
          <span className="text-slate-700">{evidence.metric.replace(/_/g, " ")} anomaly</span>
          <span className="text-xs text-slate-400">z = {evidence.peak_z_score.toFixed(1)}</span>
        </div>

        <div className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-3">
          <TokenStat label="Baseline (typical)" value={Math.round(evidence.baseline_median)} />
          <TokenStat label="Peak observed" value={Math.round(evidence.peak_value)} />
          {evidence.error_rate_spike_pct > 0 && (
            <div>
              <p className="text-xs text-slate-400">Error rate spike</p>
              <p className="font-medium text-red-600">+{formatPercent(evidence.error_rate_spike_pct)}</p>
            </div>
          )}
        </div>

        {evidence.hourly_series.length > 0 && (
          <div>
            <p className="mb-1 text-xs font-medium uppercase tracking-wide text-slate-500">
              Hourly {evidence.metric.replace(/_/g, " ")} during the anomalous window
            </p>
            <BarChart
              data={evidence.hourly_series.map((p) => ({
                label: p.bucket.slice(11, 16),
                value:
                  evidence.metric === "cost"
                    ? p.cost_micro
                    : evidence.metric === "tokens"
                      ? p.total_tokens
                      : evidence.metric === "error_rate"
                        ? p.error_rate
                        : p.requests,
              }))}
              valueLabel={(v) => formatNumber(Math.round(v))}
            />
          </div>
        )}

        <p className="rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-800">
          This is a stability signal, not a cost-savings opportunity — Fluxen never attaches a dollar
          estimate to an anomaly. Investigate the underlying traffic in Requests before deciding whether
          action is needed.
        </p>
      </div>
    </Section>
  );
}

function ImpactSection({ opportunity }: { opportunity: Opportunity }) {
  // Traffic anomaly candidates carry no cost/savings figures at all
  // (Part G.3.4) — their entire story already lives in the Evidence
  // section above, so Impact simply doesn't render for this kind rather
  // than showing three zeroed-out dollar figures.
  if (opportunity.kind === "traffic_anomaly") {
    return null;
  }

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
  if (opportunity.kind === "model_cost") {
    return <ModelMixSimulateSection opportunity={opportunity} />;
  }
  if (opportunity.kind === "repeated_request") {
    return <CachingSimulateSection opportunity={opportunity} />;
  }
  if (opportunity.kind === "traffic_anomaly") {
    return null; // no scenario applies to a stability signal
  }
  return (
    <Section title="Simulate">
      <p className="text-sm text-slate-500">No simulation scenario for this opportunity kind yet.</p>
    </Section>
  );
}

// CachingSimulateSection lets a Repeated Request opportunity run the
// exact same exact-caching replay internal/sim already supports (Phase
// 4), pre-filled with the detector's own recommended TTL. Apply is
// intentionally absent here: policy.Applier only patches the routing
// control (Phase 5's scope) — extending Apply to a caching patch is a
// real backend gap this pass does not close, so the button below is
// disabled with an honest explanation rather than a half-working flow.
function CachingSimulateSection({ opportunity }: { opportunity: Opportunity }) {
  const recommendation = opportunity.recommendation as RepeatedRequestRecommendation;

  const [ttlSeconds, setTtlSeconds] = useState(recommendation.recommended_ttl_seconds);
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
        scenario: { type: "exact_caching", ttl_seconds: ttlSeconds },
      });
      setResult(sim);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to run simulation.");
    } finally {
      setRunning(false);
    }
  }

  return (
    <Section title="Simulate">
      <div className="space-y-4">
        <div>
          <label className="mb-1 block text-xs font-medium text-slate-600">Cache TTL</label>
          <select
            className="rounded-md border border-slate-300 px-3 py-2 text-sm"
            value={ttlSeconds}
            onChange={(e) => setTtlSeconds(Number(e.target.value))}
          >
            <option value={300}>5 minutes</option>
            <option value={3600}>1 hour</option>
            <option value={21600}>6 hours</option>
            <option value={86400}>24 hours</option>
          </select>
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
              would have been served from cache.
            </p>
          </div>
        )}

        <div className="border-t border-slate-100 pt-3">
          <button
            disabled
            title="Applying a caching change isn't available yet — only model routing changes can be applied in this release."
            className="cursor-not-allowed rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-400"
          >
            Apply
          </button>
        </div>
      </div>
    </Section>
  );
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

        <div className="border-t border-slate-100 pt-3">
          {result ? (
            <ApplyAction opportunity={opportunity} simulation={result} weight={weight} recommendation={recommendation} />
          ) : (
            <button
              disabled
              title="Run a simulation first"
              className="cursor-not-allowed rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-400"
            >
              Apply
            </button>
          )}
        </div>
      </div>
    </Section>
  );
}

// ApplyAction is Rule 19's confirm dialog: the application's own slug
// must be re-typed for a routing change ("irreversible-feeling actions
// get friction on purpose") before POST .../apply fires with
// confirm: true.
function ApplyAction({
  opportunity,
  simulation,
  weight,
  recommendation,
}: {
  opportunity: Opportunity;
  simulation: Simulation;
  weight: number;
  recommendation: ModelCostRecommendation;
}) {
  const [open, setOpen] = useState(false);
  const [slug, setSlug] = useState("");
  const [appSlug, setAppSlug] = useState<string | null>(null);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [applied, setApplied] = useState(false);

  useEffect(() => {
    if (!open || appSlug) return;
    api
      .listApplications()
      .then((apps) => setAppSlug(apps.find((a) => a.id === opportunity.app_id)?.slug ?? null))
      .catch(() => setAppSlug(null));
  }, [open, appSlug, opportunity.app_id]);

  async function confirmApply() {
    if (!appSlug || slug !== appSlug) return;
    setApplying(true);
    setError(null);
    try {
      await api.applyOpportunity(opportunity.id, {
        confirm: true,
        simulation_id: simulation.id,
        routing: {
          from_model: recommendation.current_model, to_model: recommendation.candidate_model,
          weight, sticky: true,
        },
      });
      setApplied(true);
      setOpen(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to apply.");
    } finally {
      setApplying(false);
    }
  }

  if (applied) {
    return (
      <div className="rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
        Applied. Fluxen is now routing {formatPercent(weight)} of {recommendation.current_model} traffic to{" "}
        {recommendation.candidate_model}.{" "}
        <Link href={`/applications/${opportunity.app_id}/policies`} className="underline">
          View policy
        </Link>
      </div>
    );
  }

  if (!open) {
    return <Button onClick={() => setOpen(true)}>Apply</Button>;
  }

  return (
    <div className="rounded-md border border-slate-300 bg-slate-50 p-4">
      <p className="mb-2 text-sm font-medium text-slate-900">Confirm applying this recommendation</p>
      <p className="mb-3 text-xs text-slate-600">
        This will start routing {formatPercent(weight)} of {recommendation.current_model} traffic to{" "}
        {recommendation.candidate_model} in production. Type the application&apos;s slug (
        <span className="font-mono">{appSlug ?? "…"}</span>) to confirm.
      </p>
      <ErrorBanner message={error} />
      <input
        className="mb-3 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
        value={slug}
        onChange={(e) => setSlug(e.target.value)}
        placeholder={appSlug ?? ""}
        disabled={!appSlug}
      />
      <div className="flex gap-2">
        <Button onClick={confirmApply} disabled={!appSlug || slug !== appSlug || applying}>
          {applying ? "Applying…" : "Confirm apply"}
        </Button>
        <Button variant="secondary" onClick={() => { setOpen(false); setSlug(""); setError(null); }}>
          Cancel
        </Button>
      </div>
    </div>
  );
}

const verdictCopy: Record<string, { label: string; tone: "good" | "warn" | "neutral" | "danger"; explain: string }> = {
  successful: { label: "Successful", tone: "good", explain: "Realized savings reached at least 70% of the estimate." },
  partial: { label: "Partial", tone: "neutral", explain: "Realized savings reached 20–70% of the estimate." },
  no_effect: { label: "No effect", tone: "neutral", explain: "Cost per 1,000 requests changed by less than 2% — no meaningful effect either way." },
  regressed: { label: "Regressed", tone: "danger", explain: "Cost per 1,000 requests increased by more than 2% since applying." },
  inconclusive: { label: "Inconclusive", tone: "warn", explain: "Not enough clean signal yet to call this one way or the other." },
};

// MeasureSection only appears once an opportunity has actually been
// applied — Part I.2: "Measure — appears only once status = applied."
// It shows collecting/interim/final states, the before/after comparison,
// and — only on a regressed verdict — the one-click Revert (Part G.5).
function MeasureSection({ opportunity }: { opportunity: Opportunity }) {
  const [measurement, setMeasurement] = useState<Measurement | null | "none">(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (opportunity.status !== "applied" && opportunity.status !== "reverted") {
      setMeasurement("none");
      return;
    }
    let cancelled = false;
    api
      .getOpportunityMeasurement(opportunity.id)
      .then((m) => {
        if (!cancelled) setMeasurement(m);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 404) {
          setMeasurement("none");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load measurement.");
      });
    return () => {
      cancelled = true;
    };
  }, [opportunity.id, opportunity.status]);

  if (measurement === "none") {
    return null; // not applied (yet) — Measure simply doesn't exist here
  }

  return (
    <Section title="Measure">
      <ErrorBanner message={error} />
      {measurement === null && !error && <p className="text-sm text-slate-500">Loading…</p>}
      {measurement && <MeasureContent measurement={measurement} onReverted={setMeasurement} />}
    </Section>
  );
}

function MeasureContent({ measurement, onReverted }: { measurement: Measurement; onReverted: (m: Measurement) => void }) {
  if (measurement.status === "collecting") {
    return (
      <div className="space-y-2">
        <Badge>collecting</Badge>
        <p className="text-sm text-slate-600">
          Applied {new Date(measurement.applied_at).toLocaleDateString()}. The interim check runs 7 days after
          applying; the final verdict, 14 days after. Baseline (the 14 days before applying): est.{" "}
          {formatMoney(measurement.baseline_cost_per_1k_micro)} per 1,000 requests, from{" "}
          {formatNumber(measurement.baseline_requests)} requests.
        </p>
      </div>
    );
  }

  const verdict = measurement.verdict ? verdictCopy[measurement.verdict] : null;

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Badge tone={measurement.status === "interim" ? "neutral" : "good"}>
          {measurement.status === "interim" ? "interim result" : "final result"}
        </Badge>
        {verdict && <Badge tone={verdict.tone}>{verdict.label}</Badge>}
      </div>

      {verdict && <p className="text-sm text-slate-700">{verdict.explain}</p>}

      <div className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
        <div>
          <p className="text-xs uppercase tracking-wide text-slate-500">measured baseline / 1k</p>
          <p className="mt-0.5 text-base font-semibold text-slate-900">
            {formatMoney(measurement.baseline_cost_per_1k_micro)}
          </p>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wide text-slate-500">measured observed / 1k</p>
          <p className="mt-0.5 text-base font-semibold text-slate-900">
            {measurement.observed_cost_per_1k_micro != null ? formatMoney(measurement.observed_cost_per_1k_micro) : "—"}
          </p>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wide text-slate-500">realized savings</p>
          <p className="mt-0.5 text-base font-semibold text-slate-900">
            {measurement.actual_savings_micro != null ? formatMoney(measurement.actual_savings_micro) : "—"}
          </p>
        </div>
        <div>
          <p className="text-xs uppercase tracking-wide text-slate-500">est. expected</p>
          <p className="mt-0.5 text-base font-semibold text-slate-900">{formatPercent(measurement.expected_pct)}</p>
        </div>
      </div>

      <p className="text-xs text-slate-400">
        Observed over {measurement.observed_requests != null ? formatNumber(measurement.observed_requests) : "—"}{" "}
        requests
        {measurement.observed_start && measurement.observed_end
          ? ` (${measurement.observed_start.slice(0, 10)} – ${measurement.observed_end.slice(0, 10)})`
          : ""}
        . Compared as cost per 1,000 requests, not raw totals, since traffic volume always moves.
      </p>

      {measurement.verdict === "regressed" && measurement.status !== "reverted" && (
        <RevertAction measurement={measurement} onReverted={onReverted} />
      )}
      {measurement.status === "reverted" && (
        <p className="rounded-md bg-slate-100 px-3 py-2 text-xs text-slate-600">
          This change was reverted — the policy has been restored to what it was before applying.
        </p>
      )}
    </div>
  );
}

function RevertAction({ measurement, onReverted }: { measurement: Measurement; onReverted: (m: Measurement) => void }) {
  const [open, setOpen] = useState(false);
  const [reverting, setReverting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function confirmRevert() {
    setReverting(true);
    setError(null);
    try {
      await api.revertMeasurement(measurement.id, { confirm: true, note: "reverted after a regressed verdict" });
      onReverted({ ...measurement, status: "reverted" });
      setOpen(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to revert.");
    } finally {
      setReverting(false);
    }
  }

  if (!open) {
    return (
      <Button variant="danger" onClick={() => setOpen(true)}>
        Revert
      </Button>
    );
  }

  return (
    <div className="rounded-md border border-red-200 bg-red-50 p-4">
      <p className="mb-2 text-sm font-medium text-slate-900">Revert this change?</p>
      <p className="mb-3 text-xs text-slate-600">
        This restores the policy exactly as it was before applying. The opportunity may re-open later if the
        underlying inefficiency still exists.
      </p>
      <ErrorBanner message={error} />
      <div className="flex gap-2">
        <Button variant="danger" onClick={confirmRevert} disabled={reverting}>
          {reverting ? "Reverting…" : "Confirm revert"}
        </Button>
        <Button variant="secondary" onClick={() => { setOpen(false); setError(null); }}>
          Cancel
        </Button>
      </div>
    </div>
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
