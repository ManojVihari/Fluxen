"use client";

import { TopNav } from "@/components/top-nav";
import { RequestsTable } from "@/components/requests-table";

// The Requests investigation screen (Part I.5): explicitly not a tracing
// platform — no span trees, no waterfalls, just a filterable, cursor-
// paginated table over the raw `requests` fact table plus a detail
// drawer.
export default function RequestsPage() {
  return (
    <>
      <TopNav />
      <main className="mx-auto max-w-5xl p-8">
        <div className="mb-6">
          <h1 className="text-xl font-semibold text-slate-900">Requests</h1>
          <p className="text-sm text-slate-500">
            Investigation only — filter and inspect individual requests across every application.
          </p>
        </div>
        <RequestsTable />
      </main>
    </>
  );
}
