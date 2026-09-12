"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError, type PricingCatalog } from "@/lib/api";
import { ErrorBanner } from "@/components/ui";
import { formatMoney } from "@/lib/format";

// Settings > Pricing (Part I.6): view the catalog, view the Ollama
// local-cost label. Per-org editable price overrides are named in the
// same spec line but never defined anywhere else in the specification
// (no data model, no API contract, no interaction rule with Part H.2's
// "catalog updates never rewrite history") — a real, undefined feature
// this pass does not invent a data model for. This screen ships the
// unambiguous half: a read-only view of what Fluxen actually charges
// against.
export default function PricingSettingsPage() {
  const router = useRouter();
  const [catalog, setCatalog] = useState<PricingCatalog | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .getPricingCatalog()
      .then(setCatalog)
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load the pricing catalog.");
      });
  }, [router]);

  return (
    <div>
      <ErrorBanner message={error} />

      {catalog === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {catalog && (
        <div className="space-y-4">
          <p className="text-xs text-slate-500">Catalog version {catalog.version}.</p>

          <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                  <th className="px-4 py-2 font-medium">Model</th>
                  <th className="px-4 py-2 font-medium">Provider</th>
                  <th className="px-4 py-2 font-medium">Tier</th>
                  <th className="px-4 py-2 font-medium text-right">Input / 1M tok</th>
                  <th className="px-4 py-2 font-medium text-right">Output / 1M tok</th>
                </tr>
              </thead>
              <tbody>
                {catalog.models.map((m) => (
                  <tr key={m.id} className="border-b border-slate-100 last:border-0">
                    <td className="px-4 py-3 font-medium text-slate-900">{m.id}</td>
                    <td className="px-4 py-3 text-slate-600">{m.provider}</td>
                    <td className="px-4 py-3 text-slate-600">{m.tier}</td>
                    <td className="px-4 py-3 text-right">{formatMoney(m.input_per_mtok_micro)}</td>
                    <td className="px-4 py-3 text-right">{formatMoney(m.output_per_mtok_micro)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <p className="rounded-md bg-slate-50 px-3 py-2 text-xs text-slate-600">{catalog.ollama_note}</p>
        </div>
      )}
    </div>
  );
}
