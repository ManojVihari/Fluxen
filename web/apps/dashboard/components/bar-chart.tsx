"use client";

// A minimal, dependency-free bar chart — Phase 2's timeseries only needs
// one simple visualization, so this stays inline SVG rather than pulling
// in a charting library (Rule 5: no dependency without a concrete need).
// A real charting package can replace this later if the dashboard's chart
// vocabulary grows.
export function BarChart({
  data,
  valueLabel,
}: {
  data: { label: string; value: number }[];
  valueLabel: (v: number) => string;
}) {
  if (data.length === 0) {
    return <p className="text-sm text-slate-500">No data in this range yet.</p>;
  }

  const max = Math.max(...data.map((d) => d.value), 1);
  const height = 140;
  const barWidth = 100 / data.length;

  return (
    <div>
      <svg viewBox={`0 0 100 ${height}`} preserveAspectRatio="none" className="h-36 w-full">
        {data.map((d, i) => {
          const barHeight = (d.value / max) * (height - 4);
          return (
            <rect
              key={i}
              x={i * barWidth + barWidth * 0.15}
              y={height - barHeight}
              width={barWidth * 0.7}
              height={barHeight}
              className="fill-slate-700"
            >
              <title>
                {d.label}: {valueLabel(d.value)}
              </title>
            </rect>
          );
        })}
      </svg>
      <div className="mt-1 flex justify-between text-xs text-slate-400">
        <span>{data[0]?.label}</span>
        <span>{data[data.length - 1]?.label}</span>
      </div>
    </div>
  );
}
