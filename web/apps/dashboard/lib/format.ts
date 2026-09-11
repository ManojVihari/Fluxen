// Small formatting helpers. Money always comes from the API as an integer
// of micro-USD (1 USD = 1_000_000) — this is the only place that ever
// divides it back down to dollars for display; nowhere else in the UI
// should do money arithmetic (Part C.3: "Never use float64 for a cost
// figure").
export function formatMoney(micro: number): string {
  const dollars = micro / 1_000_000;
  return dollars.toLocaleString("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: dollars < 10 ? 4 : 2,
    maximumFractionDigits: dollars < 10 ? 4 : 2,
  });
}

export function formatNumber(n: number): string {
  return n.toLocaleString("en-US");
}

export function formatCompactNumber(n: number): string {
  return new Intl.NumberFormat("en-US", { notation: "compact" }).format(n);
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export function formatPercent(fraction: number): string {
  return `${(fraction * 100).toFixed(1)}%`;
}

export function formatDay(iso: string): string {
  const d = new Date(iso + "T00:00:00Z");
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}
