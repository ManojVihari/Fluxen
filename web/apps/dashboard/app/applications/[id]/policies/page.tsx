"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import {
  api,
  ApiError,
  type BudgetPolicy,
  type CachingPolicy,
  type ModelRestrictionPolicy,
  type PolicyDocument,
  type PolicyHistoryEntry,
  type PolicyResponse,
  type RateLimitPolicy,
  type RoutingPolicy,
} from "@/lib/api";
import { Button, ErrorBanner, inputClass } from "@/components/ui";
import { formatMoney } from "@/lib/format";

// Application Detail's Policies editor (Part I.4): the five controls,
// a diff-before-save, an optional note, writes to history. This is the
// same document GET/PUT /applications/{id}/policy the Apply action
// (Optimizations detail page) also writes to — editing here and
// applying an opportunity are two paths into one policy.
export default function PoliciesPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [current, setCurrent] = useState<PolicyResponse | null>(null);
  const [draft, setDraft] = useState<PolicyDocument | null>(null);
  const [history, setHistory] = useState<PolicyHistoryEntry[] | null>(null);
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [savedMsg, setSavedMsg] = useState<string | null>(null);

  function load() {
    setError(null);
    Promise.all([api.getPolicy(params.id), api.getPolicyHistory(params.id)])
      .then(([policy, hist]) => {
        setCurrent(policy);
        setDraft(policy.document);
        setHistory(hist);
      })
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load policy.");
      });
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.id]);

  async function save() {
    if (!current || !draft) return;
    setSaving(true);
    setError(null);
    setSavedMsg(null);
    try {
      const saved = await api.putPolicy(params.id, { document: draft, expected_version: current.version, note: note || undefined });
      setCurrent(saved);
      setDraft(saved.document);
      setNote("");
      setSavedMsg(`Saved as version ${saved.version}.`);
      const hist = await api.getPolicyHistory(params.id);
      setHistory(hist);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError("This policy was changed by someone else since you loaded it. Reload and try again.");
      } else {
        setError(err instanceof ApiError ? err.message : "Failed to save policy.");
      }
    } finally {
      setSaving(false);
    }
  }

  const dirty = current && draft && JSON.stringify(current.document) !== JSON.stringify(draft);

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-sm font-medium text-slate-700">Policies</h2>
        {current && <span className="text-xs text-slate-400">version {current.version}</span>}
      </div>

      <ErrorBanner message={error} />
      {savedMsg && <p className="mb-4 text-sm text-emerald-700">{savedMsg}</p>}

      {!draft && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {draft && (
        <div className="space-y-4">
          <RoutingSection value={draft.routing ?? null} onChange={(v) => setDraft({ ...draft, routing: v })} />
          <CachingSection value={draft.caching ?? null} onChange={(v) => setDraft({ ...draft, caching: v })} />
          <BudgetSection value={draft.budget ?? null} onChange={(v) => setDraft({ ...draft, budget: v })} />
          <RateLimitSection value={draft.rate_limit ?? null} onChange={(v) => setDraft({ ...draft, rate_limit: v })} />
          <ModelRestrictionSection value={draft.model_restriction ?? null} onChange={(v) => setDraft({ ...draft, model_restriction: v })} />

          {dirty && (
            <div className="rounded-lg border border-amber-200 bg-amber-50 p-4">
              <p className="mb-2 text-xs font-medium uppercase tracking-wide text-amber-800">Unsaved changes</p>
              <input
                className={inputClass}
                placeholder="Optional note (why this change?)"
                value={note}
                onChange={(e) => setNote(e.target.value)}
              />
              <div className="mt-3 flex gap-2">
                <Button onClick={save} disabled={saving}>
                  {saving ? "Saving…" : "Save changes"}
                </Button>
                <Button variant="secondary" onClick={() => { setDraft(current!.document); setNote(""); }}>
                  Discard
                </Button>
              </div>
            </div>
          )}

          {history && history.length > 0 && <HistorySection history={history} />}
        </div>
      )}
    </div>
  );
}

function ControlCard({
  title,
  description,
  enabled,
  onToggle,
  children,
}: {
  title: string;
  description: string;
  enabled: boolean;
  onToggle: (enabled: boolean) => void;
  children?: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-slate-900">{title}</p>
          <p className="text-xs text-slate-500">{description}</p>
        </div>
        <label className="flex shrink-0 items-center gap-2 text-xs text-slate-600">
          <input type="checkbox" checked={enabled} onChange={(e) => onToggle(e.target.checked)} />
          {enabled ? "Enabled" : "Disabled"}
        </label>
      </div>
      {enabled && children && <div className="mt-3 space-y-2">{children}</div>}
    </div>
  );
}

function RoutingSection({ value, onChange }: { value: RoutingPolicy | null; onChange: (v: RoutingPolicy | null) => void }) {
  const v = value ?? { enabled: false, from_model: "", to_model: "", weight: 0.5, sticky: true };
  return (
    <ControlCard
      title="Model routing"
      description="Percentage-based routing from one model to another."
      enabled={v.enabled}
      onToggle={(enabled) => onChange({ ...v, enabled })}
    >
      <div className="grid grid-cols-2 gap-2 text-sm">
        <input className={inputClass} placeholder="From model (e.g. gpt-4o)" value={v.from_model} onChange={(e) => onChange({ ...v, from_model: e.target.value })} />
        <input className={inputClass} placeholder="To model (e.g. gpt-4o-mini)" value={v.to_model} onChange={(e) => onChange({ ...v, to_model: e.target.value })} />
      </div>
      <label className="block text-xs text-slate-600">
        Weight to candidate: {(v.weight * 100).toFixed(0)}%
        <input type="range" min={0} max={1} step={0.05} value={v.weight} onChange={(e) => onChange({ ...v, weight: Number(e.target.value) })} className="mt-1 w-full" />
      </label>
      <label className="flex items-center gap-2 text-xs text-slate-600">
        <input type="checkbox" checked={v.sticky} onChange={(e) => onChange({ ...v, sticky: e.target.checked })} />
        Sticky by API key (same caller always gets the same variant)
      </label>
    </ControlCard>
  );
}

function CachingSection({ value, onChange }: { value: CachingPolicy | null; onChange: (v: CachingPolicy | null) => void }) {
  const v = value ?? { enabled: false, ttl_seconds: 3600 };
  return (
    <ControlCard title="Exact caching" description="Cache identical requests for a TTL." enabled={v.enabled} onToggle={(enabled) => onChange({ ...v, enabled })}>
      <label className="block text-xs text-slate-600">
        TTL (seconds)
        <input type="number" min={1} className={inputClass} value={v.ttl_seconds} onChange={(e) => onChange({ ...v, ttl_seconds: Number(e.target.value) })} />
      </label>
    </ControlCard>
  );
}

function BudgetSection({ value, onChange }: { value: BudgetPolicy | null; onChange: (v: BudgetPolicy | null) => void }) {
  const v = value ?? { enabled: false, period: "daily" as const, limit_micro: 10_000_000, mode: "hard" as const };
  return (
    <ControlCard title="Budget" description="Application spending limit per period." enabled={v.enabled} onToggle={(enabled) => onChange({ ...v, enabled })}>
      <div className="grid grid-cols-2 gap-2 text-sm">
        <select className={inputClass} value={v.period} onChange={(e) => onChange({ ...v, period: e.target.value as "daily" | "monthly" })}>
          <option value="daily">Daily</option>
          <option value="monthly">Monthly</option>
        </select>
        <select className={inputClass} value={v.mode} onChange={(e) => onChange({ ...v, mode: e.target.value as "hard" | "soft" })}>
          <option value="hard">Hard (blocks requests)</option>
          <option value="soft">Soft (tracks only)</option>
        </select>
      </div>
      <label className="block text-xs text-slate-600">
        Limit ({formatMoney(v.limit_micro)})
        <input
          type="number"
          min={0}
          step={0.01}
          className={inputClass}
          value={v.limit_micro / 1_000_000}
          onChange={(e) => onChange({ ...v, limit_micro: Math.round(Number(e.target.value) * 1_000_000) })}
        />
      </label>
    </ControlCard>
  );
}

function RateLimitSection({ value, onChange }: { value: RateLimitPolicy | null; onChange: (v: RateLimitPolicy | null) => void }) {
  const v = value ?? { enabled: false, requests_per_minute: 60 };
  return (
    <ControlCard title="Rate limit" description="Application request limit per minute." enabled={v.enabled} onToggle={(enabled) => onChange({ ...v, enabled })}>
      <label className="block text-xs text-slate-600">
        Requests per minute
        <input type="number" min={1} className={inputClass} value={v.requests_per_minute} onChange={(e) => onChange({ ...v, requests_per_minute: Number(e.target.value) })} />
      </label>
    </ControlCard>
  );
}

function ModelRestrictionSection({ value, onChange }: { value: ModelRestrictionPolicy | null; onChange: (v: ModelRestrictionPolicy | null) => void }) {
  const v = value ?? { enabled: false, allowed_models: [] };
  return (
    <ControlCard title="Model restrictions" description="Restrict this application to approved models." enabled={v.enabled} onToggle={(enabled) => onChange({ ...v, enabled })}>
      <label className="block text-xs text-slate-600">
        Allowed models (comma-separated)
        <input
          className={inputClass}
          value={v.allowed_models.join(", ")}
          onChange={(e) => onChange({ ...v, allowed_models: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })}
        />
      </label>
    </ControlCard>
  );
}

function HistorySection({ history }: { history: PolicyHistoryEntry[] }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <p className="mb-3 text-xs font-medium uppercase tracking-wide text-slate-500">History</p>
      <ul className="space-y-2 text-xs text-slate-600">
        {history.map((h) => (
          <li key={h.id} className="border-b border-slate-100 pb-2 last:border-0">
            <div className="flex items-center justify-between">
              <span className="font-medium text-slate-900">v{h.version} · {h.change_source}</span>
              <span>{new Date(h.changed_at).toLocaleString()}</span>
            </div>
            {h.note && <p className="mt-0.5 text-slate-500">{h.note}</p>}
            {Object.keys(h.diff ?? {}).length > 0 && (
              <p className="mt-0.5 text-slate-400">changed: {Object.keys(h.diff).join(", ")}</p>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
