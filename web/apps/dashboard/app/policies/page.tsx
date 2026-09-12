"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, ApiError, type Application, type PolicyDocument } from "@/lib/api";
import { Badge, ErrorBanner } from "@/components/ui";
import { TopNav } from "@/components/top-nav";

type Row = Application & { policy?: PolicyDocument };

// The Policies matrix (Part I.4): cross-app view of controls, one
// compact state chip per control per application. The per-app editor
// itself already exists at Application Detail's Policies tab (Phase 5);
// this is purely the read-only overview that links into it. Like the
// Applications list's own per-app summary fetches, this pulls each
// app's policy individually — fine at demo/early-adopter scale, the
// same tradeoff already made there.
export default function PoliciesMatrixPage() {
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
        const withPolicies = await Promise.all(
          apps.map(async (app) => {
            try {
              const policy = await api.getPolicy(app.id);
              return { ...app, policy: policy.document };
            } catch {
              return { ...app };
            }
          })
        );
        if (!cancelled) setRows(withPolicies);
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

  return (
    <>
      <TopNav />
      <main className="mx-auto max-w-4xl p-8">
        <div className="mb-6">
          <h1 className="text-xl font-semibold text-slate-900">Policies</h1>
          <p className="text-sm text-slate-500">Every application's controls, at a glance.</p>
        </div>

        <ErrorBanner message={error} />

        {rows === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

        {rows?.length === 0 && <p className="text-sm text-slate-500">No applications yet.</p>}

        {rows && rows.length > 0 && (
          <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                  <th className="px-4 py-2 font-medium">Application</th>
                  <th className="px-4 py-2 font-medium">Routing</th>
                  <th className="px-4 py-2 font-medium">Cache</th>
                  <th className="px-4 py-2 font-medium">Budget</th>
                  <th className="px-4 py-2 font-medium">Rate limit</th>
                  <th className="px-4 py-2 font-medium">Model restriction</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((app) => (
                  <tr key={app.id} className="border-b border-slate-100 last:border-0">
                    <td className="px-4 py-3">
                      <Link href={`/applications/${app.id}/policies`} className="font-medium text-slate-900 hover:underline">
                        {app.name}
                      </Link>
                    </td>
                    <td className="px-4 py-3">
                      <ControlChip enabled={!!app.policy?.routing?.enabled} />
                    </td>
                    <td className="px-4 py-3">
                      <ControlChip enabled={!!app.policy?.caching?.enabled} />
                    </td>
                    <td className="px-4 py-3">
                      <ControlChip enabled={!!app.policy?.budget?.enabled} />
                    </td>
                    <td className="px-4 py-3">
                      <ControlChip enabled={!!app.policy?.rate_limit?.enabled} />
                    </td>
                    <td className="px-4 py-3">
                      <ControlChip enabled={!!app.policy?.model_restriction?.enabled} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </main>
    </>
  );
}

function ControlChip({ enabled }: { enabled: boolean }) {
  return <Badge tone={enabled ? "good" : "neutral"}>{enabled ? "on" : "off"}</Badge>;
}
