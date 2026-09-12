// Shared status/severity → label+color mapping for opportunities, used
// by both the Optimizations list and detail pages so a status always
// reads the same way wherever it appears. Colors are chosen by meaning,
// not by state-machine position: "applied" is good news (green),
// "dismissed" is a neutral fact (gray), "reverted" is a caution (amber) —
// never just alternating tones by column order.
export type BadgeTone = "neutral" | "warn" | "good" | "danger" | "info";

export const STATUS_STYLE: Record<string, { label: string; tone: BadgeTone }> = {
  open: { label: "Open", tone: "info" },
  reviewed: { label: "Reviewed", tone: "neutral" },
  simulated: { label: "Simulated", tone: "info" },
  applied: { label: "Applied", tone: "good" },
  measured: { label: "Measured", tone: "good" },
  dismissed: { label: "Dismissed", tone: "neutral" },
  stale: { label: "Stale", tone: "neutral" },
  reverted: { label: "Reverted", tone: "warn" },
  failed: { label: "Failed", tone: "danger" },
};

export function statusStyle(status: string): { label: string; tone: BadgeTone } {
  return STATUS_STYLE[status] ?? { label: status, tone: "neutral" };
}

// Severity only ever reaches "high" today (internal/detect/anomaly.go),
// but the mapping covers "critical" too since the schema doesn't rule it
// out — high severity is danger (red), not warn (amber), since it's a
// materially worse signal than "medium," not just a milder version of
// the same caution.
export function severityStyle(severity: string): { label: string; tone: BadgeTone } {
  switch (severity) {
    case "critical":
    case "high":
      return { label: severity, tone: "danger" };
    case "low":
      return { label: severity, tone: "neutral" };
    default:
      return { label: severity, tone: "warn" };
  }
}

// Confidence: high confidence is good news (the recommendation is
// well-supported), low confidence is worth a caveat, medium is neither —
// distinct semantics from severity, so kept as its own function even
// though the tone values overlap for "high".
export function confidenceStyle(confidence: string): { label: string; tone: BadgeTone } {
  switch (confidence) {
    case "high":
      return { label: confidence, tone: "good" };
    case "low":
      return { label: confidence, tone: "warn" };
    default:
      return { label: confidence, tone: "neutral" };
  }
}
