"use client";

import { useRouter, useSearchParams } from "next/navigation";
import type { RangeValue } from "@/lib/api";

const OPTIONS: { value: RangeValue; label: string }[] = [
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "90d", label: "90d" },
];

// A tiny global time-range picker synced to the URL (?range=), so a tab
// switch or reload preserves the selected window — the convention Part
// I.7 describes for the eventual full dashboard, started here since
// Application Detail is the first screen that needs it.
export function RangePicker() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const current = (searchParams.get("range") as RangeValue) || "30d";

  function setRange(value: RangeValue) {
    const params = new URLSearchParams(searchParams.toString());
    params.set("range", value);
    router.push(`?${params.toString()}`);
  }

  return (
    <div className="inline-flex rounded-md border border-slate-300 bg-white p-0.5">
      {OPTIONS.map((opt) => (
        <button
          key={opt.value}
          onClick={() => setRange(opt.value)}
          className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
            current === opt.value
              ? "bg-slate-900 text-white"
              : "text-slate-600 hover:bg-slate-100"
          }`}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}

export function useRangeParam(): RangeValue {
  const searchParams = useSearchParams();
  return (searchParams.get("range") as RangeValue) || "30d";
}
