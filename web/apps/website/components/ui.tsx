import Link from "next/link";
import type { ReactNode } from "react";

export function Kicker({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700">
      {children}
    </span>
  );
}

export function PageHero({
  eyebrow,
  title,
  subtitle,
}: {
  eyebrow?: string;
  title: string;
  subtitle?: string;
}) {
  return (
    <div className="relative overflow-hidden bg-grid-fade">
      <div className="mx-auto max-w-3xl px-6 pb-14 pt-20 text-center">
        {eyebrow && (
          <div className="mb-5 flex justify-center">
            <Kicker>{eyebrow}</Kicker>
          </div>
        )}
        <h1 className="text-4xl font-semibold tracking-tight text-slate-900 sm:text-5xl sm:leading-[1.1]">
          {title}
        </h1>
        {subtitle && <p className="mx-auto mt-5 max-w-2xl text-lg leading-relaxed text-slate-600">{subtitle}</p>}
      </div>
    </div>
  );
}

export function Section({ title, kicker, children, id }: { title?: string; kicker?: string; children: ReactNode; id?: string }) {
  return (
    <section id={id} className="mx-auto max-w-5xl px-6 py-14">
      {kicker && <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-emerald-600">{kicker}</p>}
      {title && <h2 className="mb-8 text-2xl font-semibold tracking-tight text-slate-900 sm:text-3xl">{title}</h2>}
      {children}
    </section>
  );
}

export function Card({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="group rounded-2xl border border-slate-200 bg-white p-6 transition-all duration-200 hover:-translate-y-0.5 hover:border-emerald-200 hover:shadow-lg hover:shadow-emerald-950/5">
      <h3 className="mb-2 text-base font-semibold text-slate-900">{title}</h3>
      <div className="text-sm leading-relaxed text-slate-600">{children}</div>
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

export function Button({
  href,
  children,
  variant = "primary",
  icon,
}: {
  href: string;
  children: ReactNode;
  variant?: "primary" | "secondary";
  icon?: ReactNode;
}) {
  const base =
    "inline-flex items-center gap-2 rounded-lg px-5 py-2.5 text-sm font-medium transition-all duration-150";
  const variants = {
    primary:
      "bg-slate-900 text-white shadow-sm hover:bg-slate-800 hover:shadow-md",
    secondary:
      "border border-slate-300 text-slate-700 hover:border-slate-400 hover:bg-slate-50",
  };
  const className = `${base} ${variants[variant]}`;
  const external = /^https?:\/\//.test(href);

  if (external) {
    return (
      <a href={href} target="_blank" rel="noreferrer" className={className}>
        {icon}
        {children}
      </a>
    );
  }
  return (
    <Link href={href} className={className}>
      {icon}
      {children}
    </Link>
  );
}

export function CodeBlock({ children, label }: { children: string; label?: string }) {
  return (
    <div className="overflow-hidden rounded-xl border border-slate-800 bg-slate-950 shadow-sm">
      {label && (
        <div className="flex items-center gap-1.5 border-b border-slate-800 px-4 py-2">
          <span className="h-2.5 w-2.5 rounded-full bg-slate-700" />
          <span className="h-2.5 w-2.5 rounded-full bg-slate-700" />
          <span className="h-2.5 w-2.5 rounded-full bg-slate-700" />
          <span className="ml-2 text-xs text-slate-400">{label}</span>
        </div>
      )}
      <pre className="overflow-x-auto p-4 text-sm leading-relaxed text-emerald-300">
        <code>{children}</code>
      </pre>
    </div>
  );
}

export function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <div className="flex gap-4">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-slate-900 text-sm font-semibold text-white">
        {n}
      </div>
      <div className="pb-2 pt-0.5">
        <p className="font-medium text-slate-900">{title}</p>
        <div className="mt-1 text-sm leading-relaxed text-slate-600">{children}</div>
      </div>
    </div>
  );
}
