import type { ReactNode } from "react";

export function PageHero({ eyebrow, title, subtitle }: { eyebrow?: string; title: string; subtitle?: string }) {
  return (
    <div className="mx-auto max-w-3xl px-6 pb-12 pt-16 text-center">
      {eyebrow && <p className="mb-3 text-sm font-medium uppercase tracking-wide text-slate-500">{eyebrow}</p>}
      <h1 className="text-4xl font-semibold tracking-tight text-slate-900 sm:text-5xl">{title}</h1>
      {subtitle && <p className="mx-auto mt-4 max-w-2xl text-lg text-slate-600">{subtitle}</p>}
    </div>
  );
}

export function Section({ title, children, id }: { title?: string; children: ReactNode; id?: string }) {
  return (
    <section id={id} className="mx-auto max-w-5xl px-6 py-12">
      {title && <h2 className="mb-6 text-2xl font-semibold text-slate-900">{title}</h2>}
      {children}
    </section>
  );
}

export function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-6">
      <h3 className="mb-2 text-base font-semibold text-slate-900">{title}</h3>
      <div className="text-sm text-slate-600">{children}</div>
    </div>
  );
}

export function Grid({ children, cols = 3 }: { children: ReactNode; cols?: 2 | 3 }) {
  return (
    <div className={`grid grid-cols-1 gap-4 ${cols === 2 ? "sm:grid-cols-2" : "sm:grid-cols-3"}`}>{children}</div>
  );
}

// Check/dash cell for a capability comparison table — deliberately not a
// plain "yes/no" grid: most rows are "partial" (a category has some form
// of the capability, just not the specific shape Fluxen's closed loop
// needs), and collapsing that to yes/no would misrepresent competitors.
export function Cell({ state }: { state: "yes" | "no" | "partial" }) {
  const styles = {
    yes: "text-emerald-600",
    no: "text-slate-300",
    partial: "text-amber-600",
  };
  const glyph = { yes: "✓", no: "—", partial: "◐" }[state];
  return <span className={`text-base ${styles[state]}`}>{glyph}</span>;
}
