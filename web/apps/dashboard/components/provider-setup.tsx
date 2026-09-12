"use client";

import { useState } from "react";
import { api, ApiError, type ProviderCredential } from "@/lib/api";
import { Badge, Button, ErrorBanner, Field, inputClass } from "@/components/ui";

// Shared provider list + row, used by Settings > Providers (the full CRUD
// screen), the onboarding wizard's first step, and the Connect tab's
// inline nudge when an application has no provider to actually route to.
// One implementation of "add a credential" so all three stay consistent.
export const PROVIDERS: { value: "openai" | "gemini" | "ollama"; label: string; note?: string }[] = [
  { value: "openai", label: "OpenAI" },
  { value: "gemini", label: "Google Gemini" },
  {
    value: "ollama",
    label: "Ollama",
    note: "Translation logic is unit-tested but has not been verified against a real Ollama server — see docs/providers.md.",
  },
];

export function ProviderRow({
  provider,
  label,
  note,
  credential,
  onChange,
  defaultEditing = false,
}: {
  provider: "openai" | "gemini" | "ollama";
  label: string;
  note?: string;
  credential?: ProviderCredential;
  onChange: () => void;
  defaultEditing?: boolean;
}) {
  const [editing, setEditing] = useState(defaultEditing);
  const [apiKey, setApiKey] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [saving, setSaving] = useState(false);
  const [checking, setChecking] = useState(false);
  const [healthResult, setHealthResult] = useState<{ status: string; error: string } | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      await api.createProviderCredential({ provider, api_key: apiKey || undefined, base_url: baseURL || undefined });
      setEditing(false);
      setApiKey("");
      setBaseURL("");
      onChange();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to save credential.");
    } finally {
      setSaving(false);
    }
  }

  async function revoke() {
    if (!credential) return;
    setSaving(true);
    setError(null);
    try {
      await api.revokeProviderCredential(credential.id);
      onChange();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to revoke credential.");
    } finally {
      setSaving(false);
    }
  }

  async function checkHealth() {
    if (!credential) return;
    setChecking(true);
    setHealthResult(null);
    try {
      const result = await api.providerHealthCheck(credential.id);
      setHealthResult(result);
    } catch (err) {
      setHealthResult({ status: "error", error: err instanceof ApiError ? err.message : "Health check failed." });
    } finally {
      setChecking(false);
    }
  }

  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium text-slate-900">{label}</p>
          {credential ? (
            <p className="mt-0.5 text-xs text-slate-500">
              {credential.base_url ? `${credential.base_url} · ` : ""}
              configured {new Date(credential.created_at).toLocaleDateString()}
            </p>
          ) : (
            <p className="mt-0.5 text-xs text-slate-400">Not configured</p>
          )}
          {note && <p className="mt-1 max-w-md text-xs text-amber-700">{note}</p>}
        </div>
        <div className="flex items-center gap-2">
          {credential && <Badge tone="good">active</Badge>}
          {credential && (
            <Button variant="secondary" onClick={checkHealth} disabled={checking}>
              {checking ? "Checking…" : "Health check"}
            </Button>
          )}
          {credential ? (
            <Button variant="danger" onClick={revoke} disabled={saving}>
              Revoke
            </Button>
          ) : (
            <Button variant="secondary" onClick={() => setEditing((v) => !v)}>
              {editing ? "Cancel" : "Configure"}
            </Button>
          )}
        </div>
      </div>

      {healthResult && (
        <p className={`mt-2 text-xs ${healthResult.status === "ok" ? "text-emerald-600" : "text-red-600"}`}>
          {healthResult.status === "ok" ? "Healthy." : `Error: ${healthResult.error}`}
        </p>
      )}
      {credential?.last_health_check_status && !healthResult && (
        <p className={`mt-2 text-xs ${credential.last_health_check_status === "ok" ? "text-emerald-600" : "text-red-600"}`}>
          Last check ({new Date(credential.last_health_check_at!).toLocaleString()}):{" "}
          {credential.last_health_check_status === "ok" ? "healthy" : credential.last_health_check_error}
        </p>
      )}

      {editing && (
        <div className="mt-4 border-t border-slate-100 pt-4">
          <ErrorBanner message={error} />
          {provider !== "ollama" && (
            <Field label="API key">
              <input className={inputClass} type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
            </Field>
          )}
          <Field label={provider === "ollama" ? "Base URL (e.g. http://localhost:11434)" : "Base URL override (optional)"}>
            <input className={inputClass} value={baseURL} onChange={(e) => setBaseURL(e.target.value)} placeholder={provider === "ollama" ? "http://localhost:11434" : ""} />
          </Field>
          <Button onClick={save} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
      )}
    </div>
  );
}
