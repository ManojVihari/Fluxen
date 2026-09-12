// A handful of tiny shared primitives, not a design system — Phase 1's UI
// is intentionally utilitarian (Part L: "minimal"). Full UI conventions
// (Money/ValueBadge/etc.) arrive with the screens that need them.
import type { ReactNode } from "react";

export function Card({ children }: { children: ReactNode }) {
  return (
    <div className="w-full max-w-md rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
      {children}
    </div>
  );
}

export function PageTitle({ children }: { children: ReactNode }) {
  return <h1 className="mb-1 text-xl font-semibold text-slate-900">{children}</h1>;
}

export function Subtitle({ children }: { children: ReactNode }) {
  return <p className="mb-6 text-sm text-slate-500">{children}</p>;
}

export function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <label className="mb-4 block">
      <span className="mb-1 block text-sm font-medium text-slate-700">{label}</span>
      {children}
    </label>
  );
}

export const inputClass =
  "w-full rounded-md border border-slate-300 px-3 py-2 text-sm text-slate-900 focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500";

export function Button({
  children,
  type = "button",
  onClick,
  disabled,
  variant = "primary",
}: {
  children: ReactNode;
  type?: "button" | "submit";
  onClick?: () => void;
  disabled?: boolean;
  variant?: "primary" | "secondary" | "danger";
}) {
  const base =
    "inline-flex items-center justify-center rounded-md px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50";
  const variants: Record<string, string> = {
    primary: "bg-slate-900 text-white hover:bg-slate-700",
    secondary: "border border-slate-300 text-slate-700 hover:bg-slate-50",
    danger: "bg-red-600 text-white hover:bg-red-700",
  };
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      className={`${base} ${variants[variant]}`}
    >
      {children}
    </button>
  );
}

export function ErrorBanner({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
      {message}
    </div>
  );
}

export function CodeBlock({ children }: { children: string }) {
  return (
    <pre className="overflow-x-auto rounded-md bg-slate-900 p-3 text-xs text-slate-100">
      <code>{children}</code>
    </pre>
  );
}

export function StatTile({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <p className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</p>
      <p className="mt-1 text-2xl font-semibold text-slate-900">{value}</p>
      {sub && <p className="mt-0.5 text-xs text-slate-400">{sub}</p>}
    </div>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center text-sm text-slate-500">
      {children}
    </div>
  );
}

export function Badge({
  children,
  tone = "neutral",
  title,
}: {
  children: ReactNode;
  tone?: "neutral" | "warn" | "good" | "danger" | "info";
  title?: string;
}) {
  const tones: Record<string, string> = {
    neutral: "bg-slate-100 text-slate-600",
    warn: "bg-amber-100 text-amber-700",
    good: "bg-emerald-100 text-emerald-700",
    // danger: something bad (a regression, a high-severity signal) —
    // distinct from warn's "worth a look" and good's "this is fine",
    // never reused for "high confidence" (that's good, not a warning).
    danger: "bg-red-100 text-red-700",
    // info: a neutral-but-distinguishing label (a status like
    // "reviewed" or "dismissed" that isn't good or bad news, just a
    // fact) — visually different from plain neutral gray so it doesn't
    // read as "unknown/default."
    info: "bg-blue-100 text-blue-700",
  };
  return (
    <span title={title} className={`rounded-full px-2 py-0.5 text-xs font-medium ${tones[tone]}`}>
      {children}
    </span>
  );
}
