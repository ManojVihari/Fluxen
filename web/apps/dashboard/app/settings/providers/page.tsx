"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError, type ProviderCredential } from "@/lib/api";
import { ErrorBanner } from "@/components/ui";
import { PROVIDERS, ProviderRow } from "@/components/provider-setup";

// Settings > Providers (Part I.6): credentials CRUD + health check.
// Replaces the OPENAI_API_KEY/GEMINI_API_KEY/OLLAMA_BASE_URL env vars
// with real, encrypted-at-rest, dashboard-managed credentials.
export default function ProvidersSettingsPage() {
  const router = useRouter();
  const [creds, setCreds] = useState<ProviderCredential[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  function load() {
    api
      .listProviderCredentials()
      .then(setCreds)
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load provider credentials.");
      });
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const active = (creds ?? []).filter((c) => c.status === "active");

  return (
    <div className="space-y-6">
      <ErrorBanner message={error} />

      {creds === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {creds && (
        <div className="space-y-3">
          {PROVIDERS.map((p) => {
            const cred = active.find((c) => c.provider === p.value);
            return (
              <ProviderRow
                key={p.value}
                provider={p.value}
                label={p.label}
                note={p.note}
                credential={cred}
                onChange={load}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
