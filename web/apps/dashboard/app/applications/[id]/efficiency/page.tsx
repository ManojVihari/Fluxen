"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, ApiError, type ScoreResponse } from "@/lib/api";
import { EmptyState, ErrorBanner } from "@/components/ui";

const COMPONENTS: { key: keyof ScoreResponse; label: string }[] = [
  { key: "model_efficiency", label: "Model efficiency" },
  { key: "token_efficiency", label: "Token efficiency" },
  { key: "cache_efficiency", label: "Cache efficiency" },
  { key: "traffic_stability", label: "Traffic stability" },
  { key: "cost_efficiency", label: "Cost efficiency" },
];

// Application Detail's Efficiency tab (Part I.1, PRD §20): the real
// score ring and five-component breakdown from internal/score, now that
// Phase 7 computes one. The opportunity list itself lives on the
// Opportunities tab next to this one.
export default function ApplicationEfficiencyPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [score, setScore] = useState<ScoreResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .applicationScore(params.id)
      .then((data) => {
        if (!cancelled) setScore(data);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load the efficiency score.");
      });
    return () => {
      cancelled = true;
    };
  }, [params.id, router]);

  return (
    <div>
      <h2 className="mb-4 text-sm font-medium text-slate-700">Efficiency</h2>

      <ErrorBanner message={error} />

      {!score && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {score && score.status === "insufficient_data" && (
        <EmptyState>
          Not enough data yet. Fluxen needs at least 1,000 requests and $5 of spend over the trailing 14
          days before it will compute a score for this application.
        </EmptyState>
      )}

      {score && score.status === "ok" && (
        <div className="flex flex-col items-start gap-8 sm:flex-row">
          <ScoreRing value={score.overall} />
          <div className="grid flex-1 grid-cols-1 gap-3 sm:grid-cols-2">
            {COMPONENTS.map((c) => (
              <ComponentBar key={c.key} label={c.label} value={score[c.key] as number} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function ScoreRing({ value }: { value: number }) {
  const radius = 52;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference * (1 - value / 100);
  const tone = value >= 80 ? "stroke-emerald-500" : value >= 50 ? "stroke-amber-500" : "stroke-red-500";

  return (
    <div className="relative shrink-0" style={{ width: 140, height: 140 }}>
      <svg width={140} height={140} viewBox="0 0 140 140">
        <circle cx={70} cy={70} r={radius} strokeWidth={12} className="fill-none stroke-slate-100" />
        <circle
          cx={70}
          cy={70}
          r={radius}
          strokeWidth={12}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          transform="rotate(-90 70 70)"
          className={`fill-none transition-all ${tone}`}
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-3xl font-semibold text-slate-900">{value}</span>
        <span className="text-xs text-slate-400">/ 100</span>
      </div>
    </div>
  );
}

function ComponentBar({ label, value }: { label: string; value: number }) {
  const tone = value >= 80 ? "bg-emerald-500" : value >= 50 ? "bg-amber-500" : "bg-red-500";
  return (
    <div>
      <div className="mb-1 flex justify-between text-xs text-slate-600">
        <span>{label}</span>
        <span className="font-medium text-slate-900">{value}</span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100">
        <div className={`h-full rounded-full ${tone}`} style={{ width: `${Math.min(value, 100)}%` }} />
      </div>
    </div>
  );
}
