"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, ApiError, type Application } from "@/lib/api";
import { Button, ErrorBanner } from "@/components/ui";

// Phase 1's applications list: name and status only. Spend, requests,
// efficiency, and opportunity columns (Part I.1's full spec) arrive in
// Phase 2 and Phase 6 respectively, once there's real traffic and
// detectors to report on.
export default function ApplicationsPage() {
  const router = useRouter();
  const [apps, setApps] = useState<Application[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .listApplications()
      .then((list) => {
        if (!cancelled) setApps(list);
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

      {apps === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {apps?.length === 0 && (
        <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center">
          <p className="mb-4 text-sm text-slate-500">
            You haven&apos;t created an application yet.
          </p>
          <Link href="/applications/new">
            <Button>Create your first application</Button>
          </Link>
        </div>
      )}

      {apps && apps.length > 0 && (
        <ul className="divide-y divide-slate-200 rounded-lg border border-slate-200 bg-white">
          {apps.map((app) => (
            <li key={app.id} className="flex items-center justify-between px-4 py-3">
              <div>
                <p className="font-medium text-slate-900">{app.name}</p>
                <p className="text-xs text-slate-500">{app.slug}</p>
              </div>
              <div className="flex items-center gap-3">
                <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600">
                  {app.status}
                </span>
                <Link
                  href={`/applications/${app.id}/connect`}
                  className="text-sm font-medium text-slate-700 underline-offset-2 hover:underline"
                >
                  Connect
                </Link>
              </div>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
