"use client";

import { useParams } from "next/navigation";
import { RequestsTable } from "@/components/requests-table";

// Application Detail's Requests tab (Part I.1/I.5) — the same
// investigation table as the top-level /requests screen, fixed to this
// application.
export default function ApplicationRequestsPage() {
  const params = useParams<{ id: string }>();
  return (
    <div>
      <h2 className="mb-4 text-sm font-medium text-slate-700">Requests</h2>
      <RequestsTable appId={params.id} />
    </div>
  );
}
