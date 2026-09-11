"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, ApiError, type Application, type ApplicationSummary } from "@/lib/api";
import { Button, ErrorBanner } from "@/components/ui";
import { formatMoney, formatNumber } from "@/lib/format";

type Row = Application & { summary?: ApplicationSummary };

// The applications list: name, spend, requests (Part L Phase 2 frontend
// task — "minimal table sufficient to navigate to Application Detail").
// Efficiency and opportunity columns (Part I's full spec) arrive with
// Phase 3/6 once there's a score and detectors to report on.
export default function ApplicationsPage() {
  const router = useRouter();
  const [rows, setRows] = useState<Row[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    api
      .listApplications()
      .then(async (apps) => {
        if (cancelled) return;
        setRows(apps);

        // 30-day spend/requests per app — Phase 2 has no bulk summary
        // endpoint, so this fetches each application's own summary
        // individually. Fine at demo/early-adopter scale; a bulk
        // Overview-style endpoint is a later-phase concern once an
        // organization might have dozens of applications.
        const withSummaries = await Promise.all(
          apps.map(async (app) => {
            try {
              const summary = await api.applicationSummary(app.id, "30d");
              return { ...app, summary };
            } catch {
              return { ...app };
            }
          })
        );
        if (!cancelled) setRows(withSummaries);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load applications.");
      });

    return () => {
      cancelled = true;
    };
  }, [router]);

  async function handleLogout() {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }

  return (
    <main className="mx-auto max-w-3xl p-8">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-slate-900">Applications</h1>
          <p className="text-sm text-slate-500">The unit of optimization in Fluxen.</p>
        </div>
        <div className="flex gap-2">
          <Link href="/applications/new">
            <Button>New application</Button>
          </Link>
          <Button variant="secondary" onClick={handleLogout}>
            Log out
          </Button>
        </div>
      </div>

      <ErrorBanner message={error} />

      {rows === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {rows?.length === 0 && (
        <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center">
          <p className="mb-4 text-sm text-slate-500">
            You haven&apos;t created an application yet.
          </p>
          <Link href="/applications/new">
            <Button>Create your first application</Button>
          </Link>
        </div>
      )}

      {rows && rows.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                <th className="px-4 py-2 font-medium">Application</th>
                <th className="px-4 py-2 font-medium text-right">Spend (30d)</th>
                <th className="px-4 py-2 font-medium text-right">Requests (30d)</th>
                <th className="px-4 py-2 font-medium"></th>
              </tr>
            </thead>
            <tbody>
              {rows.map((app) => (
                <tr key={app.id} className="border-b border-slate-100 last:border-0">
                  <td className="px-4 py-3">
                    <Link href={`/applications/${app.id}`} className="font-medium text-slate-900 hover:underline">
                      {app.name}
                    </Link>
                    <p className="text-xs text-slate-500">{app.slug}</p>
                  </td>
                  <td className="px-4 py-3 text-right">
                    {app.summary ? formatMoney(app.summary.cost_micro) : "—"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    {app.summary ? formatNumber(app.summary.requests) : "—"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Link
                      href={`/applications/${app.id}/connect`}
                      className="text-sm font-medium text-slate-700 underline-offset-2 hover:underline"
                    >
                      Connect
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </main>
  );
}
