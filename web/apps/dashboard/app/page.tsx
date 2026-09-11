"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError } from "@/lib/api";

// The root route has no UI of its own — it only decides where a visitor
// belongs: the setup wizard (nobody has set up Fluxen yet), the login
// page (setup is done but this browser has no session), or the
// applications list (Part J's first-run flow).
export default function Home() {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function route() {
      try {
        const status = await api.setupStatus();
        if (cancelled) return;
        if (!status.complete) {
          router.replace("/setup");
          return;
        }

        try {
          await api.session();
          if (!cancelled) router.replace("/applications");
        } catch {
          if (!cancelled) router.replace("/login");
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof ApiError
              ? err.message
              : "Could not reach the Fluxen API. Is it running?"
          );
        }
      }
    }

    route();
    return () => {
      cancelled = true;
    };
  }, [router]);

  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-2 p-8">
      <h1 className="text-2xl font-semibold">Fluxen</h1>
      {error ? (
        <p className="max-w-sm text-center text-red-600">{error}</p>
      ) : (
        <p className="text-slate-500">Loading…</p>
      )}
    </main>
  );
}
