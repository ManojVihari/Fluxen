"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useParams, useRouter } from "next/navigation";
import { api, ApiError, type Application } from "@/lib/api";
import { ErrorBanner } from "@/components/ui";

// The Application Detail shell: persistent header + tab nav (Part I.1).
// Phase 2 wired Usage & Cost and Models to real data; Phase 3 wired
// Efficiency to "at minimum an opportunity count" (the full efficiency
// score/ring is still Phase 7); Phase 5 wires Policies to the real
// editor (Part I.4). Opportunities and Requests stay visible-but-disabled
// placeholders so the eventual tab set is legible without pretending
// those screens exist yet.
export default function ApplicationLayout({ children }: { children: React.ReactNode }) {
  const params = useParams<{ id: string }>();
  const pathname = usePathname();
  const router = useRouter();

  const [app, setApp] = useState<Application | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
    return () => {
      cancelled = true;
    };
  }, [params.id, router]);

  const base = `/applications/${params.id}`;
  const tabs = [
    { href: base, label: "Usage & Cost", active: pathname === base },
    { href: `${base}/models`, label: "Models", active: pathname === `${base}/models` },
    { href: `${base}/efficiency`, label: "Efficiency", active: pathname === `${base}/efficiency` },
    { label: "Opportunities", disabled: true },
    { href: `${base}/policies`, label: "Policies", active: pathname === `${base}/policies` },
    { label: "Requests", disabled: true },
    { href: `${base}/connect`, label: "Keys", active: pathname === `${base}/connect` },
  ];

  if (notFound) {
    return (
      <main className="mx-auto max-w-4xl p-8">
        <ErrorBanner message="Application not found." />
        <Link href="/applications" className="text-sm underline">
          Back to applications
        </Link>
      </main>
    );
  }

  return (
    <main className="mx-auto max-w-4xl p-8">
      <Link href="/applications" className="text-sm text-slate-500 underline-offset-2 hover:underline">
        ← Applications
      </Link>

      <div className="mb-4 mt-2 flex items-center justify-between">
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

      <ErrorBanner message={error} />

      <nav className="mb-6 flex gap-1 border-b border-slate-200">
        {tabs.map((tab) =>
          tab.disabled ? (
            <span
              key={tab.label}
              title="Coming in a later phase"
              className="cursor-not-allowed border-b-2 border-transparent px-3 py-2 text-sm text-slate-300"
            >
              {tab.label}
            </span>
          ) : (
            <Link
              key={tab.label}
              href={tab.href!}
              className={`border-b-2 px-3 py-2 text-sm font-medium transition-colors ${
                tab.active
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-slate-500 hover:text-slate-900"
              }`}
            >
              {tab.label}
            </Link>
          )
        )}
      </nav>

      {children}
    </main>
  );
}

function statusLabel(app: Application): string {
  if (!app.last_seen_at) return "never connected";
  const lastSeen = new Date(app.last_seen_at);
  const hoursSince = (Date.now() - lastSeen.getTime()) / 3_600_000;
  if (hoursSince > 24) return "idle";
  return "receiving traffic";
}
