"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useParams, useRouter } from "next/navigation";
import { api, ApiError, type Application, type ScoreResponse } from "@/lib/api";
import { ErrorBanner } from "@/components/ui";
import { TopNav } from "@/components/top-nav";
import { formatMoney } from "@/lib/format";

// The Application Detail shell: persistent header + tab nav (Part I.1).
// The header shows name/status, 30-day spend, the efficiency ring, and
// open-opportunity value (Part I.1's exact layout); every tab below it
// is now wired to real data as of Phase 7.
export default function ApplicationLayout({ children }: { children: React.ReactNode }) {
  const params = useParams<{ id: string }>();
  const pathname = usePathname();
  const router = useRouter();

  const [app, setApp] = useState<Application | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [spendMicro, setSpendMicro] = useState<number | null>(null);
  const [score, setScore] = useState<ScoreResponse | null>(null);
  const [opportunityValueMicro, setOpportunityValueMicro] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .listApplications()
      .then((apps) => {
        if (cancelled) return;
        const found = apps.find((a) => a.id === params.id) ?? null;
        if (!found) {
          setNotFound(true);
          return;
        }
        setApp(found);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load application.");
      });

    api.applicationSummary(params.id, "30d").then((s) => {
      if (!cancelled) setSpendMicro(s.cost_micro);
    }).catch(() => {});
    api.applicationScore(params.id).then((s) => {
      if (!cancelled) setScore(s);
    }).catch(() => {});
    api.listOpportunities({ appId: params.id, status: "open" }).then((opps) => {
      if (!cancelled) setOpportunityValueMicro(opps.reduce((sum, o) => sum + o.savings_micro, 0));
    }).catch(() => {});

    return () => {
      cancelled = true;
    };
  }, [params.id, router]);

  const base = `/applications/${params.id}`;
  const tabs = [
    { href: base, label: "Usage & Cost", active: pathname === base },
    { href: `${base}/models`, label: "Models", active: pathname === `${base}/models` },
    { href: `${base}/efficiency`, label: "Efficiency", active: pathname === `${base}/efficiency` },
    { href: `${base}/opportunities`, label: "Opportunities", active: pathname === `${base}/opportunities` },
    { href: `${base}/policies`, label: "Policies", active: pathname === `${base}/policies` },
    { href: `${base}/requests`, label: "Requests", active: pathname === `${base}/requests` },
    { href: `${base}/connect`, label: "Keys", active: pathname === `${base}/connect` },
  ];

  if (notFound) {
    return (
      <>
        <TopNav />
        <main className="mx-auto max-w-4xl p-8">
          <ErrorBanner message="Application not found." />
          <Link href="/applications" className="text-sm underline">
            Back to applications
          </Link>
        </main>
      </>
    );
  }

  return (
    <>
      <TopNav />
      <main className="mx-auto max-w-4xl p-8">
        <Link href="/applications" className="text-sm text-slate-500 underline-offset-2 hover:underline">
          ← Applications
        </Link>

        <div className="mb-1 mt-2 flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-slate-900">{app?.name ?? "…"}</h1>
            {app && <p className="text-xs text-slate-500">{app.slug}</p>}
          </div>
          {app && (
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600">
              {statusLabel(app)}
            </span>
          )}
        </div>

        {app && (
          <div className="mb-4 flex flex-wrap gap-x-6 gap-y-1 text-sm text-slate-600">
            <span>
              <span className="text-slate-400">30d spend:</span>{" "}
              {spendMicro != null ? formatMoney(spendMicro) : "—"}
            </span>
            <span>
              <span className="text-slate-400">Efficiency:</span>{" "}
              {score
                ? score.status === "ok"
                  ? `${score.overall}/100`
                  : "not enough data"
                : "—"}
            </span>
            <span>
              <span className="text-slate-400">Open opportunity value:</span>{" "}
              {opportunityValueMicro != null && opportunityValueMicro > 0
                ? `${formatMoney(opportunityValueMicro)}/mo`
                : "—"}
            </span>
          </div>
        )}

        <ErrorBanner message={error} />

        <nav className="mb-6 flex gap-1 border-b border-slate-200">
          {tabs.map((tab) => (
            <Link
              key={tab.label}
              href={tab.href}
              className={`border-b-2 px-3 py-2 text-sm font-medium transition-colors ${
                tab.active
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-slate-500 hover:text-slate-900"
              }`}
            >
              {tab.label}
            </Link>
          ))}
        </nav>

        {children}
      </main>
    </>
  );
}

function statusLabel(app: Application): string {
  if (!app.last_seen_at) return "never connected";
  const lastSeen = new Date(app.last_seen_at);
  const hoursSince = (Date.now() - lastSeen.getTime()) / 3_600_000;
  if (hoursSince > 24) return "idle";
  return "receiving traffic";
}
