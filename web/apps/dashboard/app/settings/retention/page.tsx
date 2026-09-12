"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError, type Settings } from "@/lib/api";
import { Button, ErrorBanner, Field, inputClass } from "@/components/ui";

// Settings > Retention (Part I.6/E.2): the three retention knobs — how
// long raw requests are kept, how long captured request/response bodies
// are kept (a shorter, separate window), and whether body capture is on
// at all (off by default). Enforced daily by the retention.enforce job.
export default function RetentionSettingsPage() {
  const router = useRouter();
  const [settings, setSettings] = useState<Settings | null>(null);
  const [draft, setDraft] = useState<Settings | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [savedMsg, setSavedMsg] = useState<string | null>(null);

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setSettings(s);
        setDraft(s);
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load settings.");
      });
  }, [router]);

  async function save() {
    if (!draft) return;
    setSaving(true);
    setError(null);
    setSavedMsg(null);
    try {
      const saved = await api.patchSettings(draft);
      setSettings(saved);
      setDraft(saved);
      setSavedMsg("Saved.");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to save settings.");
    } finally {
      setSaving(false);
    }
  }

  const dirty = settings && draft && JSON.stringify(settings) !== JSON.stringify(draft);

  return (
    <div className="max-w-sm">
      <ErrorBanner message={error} />

      {draft === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {draft && (
        <div className="space-y-4">
          <Field label="Requests retention (days)">
            <input
              className={inputClass}
              type="number"
              min={1}
              value={draft.requests_retention_days}
              onChange={(e) => setDraft({ ...draft, requests_retention_days: Number(e.target.value) })}
            />
          </Field>

          <div>
            <label className="flex items-center gap-2 text-sm text-slate-700">
              <input
                type="checkbox"
                checked={draft.body_capture_enabled}
                onChange={(e) => setDraft({ ...draft, body_capture_enabled: e.target.checked })}
              />
              Capture request/response bodies (off by default)
            </label>
            {draft.body_capture_enabled && (
              <p className="mt-2 rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-800">
                Captured bodies are stored exactly as sent/received — <strong>unredacted</strong>. If your
                traffic includes PII, secrets, or other sensitive content in prompts or responses, that
                content will be stored in Postgres too. Only non-streaming responses are captured; a
                streamed response's body is never stored (its SSE bytes aren&apos;t valid JSON for the
                storage column). Takes up to ~5 seconds to take effect after saving.
              </p>
            )}
          </div>

          <Field label="Body retention (days, only relevant if capture is enabled)">
            <input
              className={inputClass}
              type="number"
              min={1}
              value={draft.body_retention_days}
              onChange={(e) => setDraft({ ...draft, body_retention_days: Number(e.target.value) })}
            />
          </Field>

          {savedMsg && <p className="text-xs text-emerald-600">{savedMsg}</p>}

          <Button onClick={save} disabled={!dirty || saving}>
            {saving ? "Saving…" : "Save"}
          </Button>

          <p className="text-xs text-slate-400">
            Dismissed/stale opportunities are always dropped after 180 days; rollups, scores, measurements, and
            policy history are kept indefinitely — neither is configurable.
          </p>
        </div>
      )}
    </div>
  );
}
