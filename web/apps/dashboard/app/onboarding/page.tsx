"use client";

import { useEffect, useState, type FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, ApiError, GATEWAY_BASE_URL, type Application, type ApiKey, type ProviderCredential } from "@/lib/api";
import { Button, CodeBlock, ErrorBanner, Field, inputClass } from "@/components/ui";
import { PROVIDERS, ProviderRow } from "@/components/provider-setup";

// The three-step guide shown right after a new org is created (Part J's
// Aha-Moment journey, made explicit as its own screen rather than left
// for the user to piece together from Settings + Applications on their
// own). Every step reflects real backend state — not a client-side
// "mark as done" checkbox — so refreshing, skipping ahead, or coming back
// later all show the truth.
export default function OnboardingPage() {
  const router = useRouter();
  const [creds, setCreds] = useState<ProviderCredential[] | null>(null);
  const [apps, setApps] = useState<Application[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  function loadCreds() {
    return api.listProviderCredentials().then(setCreds);
  }
  function loadApps() {
    return api.listApplications().then(setApps);
  }

  useEffect(() => {
    Promise.all([loadCreds(), loadApps()]).catch((err) => {
      if (err instanceof ApiError && err.status === 401) {
        router.replace("/login");
        return;
      }
      setError(err instanceof ApiError ? err.message : "Failed to load onboarding status.");
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const providerConfigured = (creds ?? []).some((c) => c.status === "active");
  const app = apps?.[0];
  const loading = creds === null || apps === null;

  return (
    <main className="mx-auto max-w-2xl p-8">
      <div className="mb-8 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-slate-900">Get Fluxen running</h1>
          <p className="mt-1 text-sm text-slate-500">Three steps: connect a provider, create an application, point your code at it.</p>
        </div>
        <Link href="/applications" className="text-sm text-slate-500 underline-offset-2 hover:underline">
          Skip for now
        </Link>
      </div>

      <ErrorBanner message={error} />

      {loading && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {!loading && (
        <div className="space-y-4">
          <StepConfigureProvider done={providerConfigured} creds={creds!} onChange={loadCreds} />
          <StepCreateApplication done={!!app} app={app} onCreated={loadApps} unlocked={providerConfigured} />
          <StepConnect app={app} unlocked={!!app} />
        </div>
      )}
    </main>
  );
}

function StepShell({
  index,
  title,
  done,
  unlocked = true,
  children,
}: {
  index: number;
  title: string;
  done: boolean;
  unlocked?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className={`rounded-lg border p-5 ${done ? "border-emerald-200 bg-emerald-50/40" : "border-slate-200 bg-white"} ${!unlocked ? "opacity-50" : ""}`}>
      <div className="mb-3 flex items-center gap-3">
        <span
          className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${
            done ? "bg-emerald-600 text-white" : "bg-slate-200 text-slate-600"
          }`}
        >
          {done ? "✓" : index}
        </span>
        <h2 className="text-sm font-semibold text-slate-900">{title}</h2>
      </div>
      {unlocked && children}
      {!unlocked && <p className="text-xs text-slate-400">Complete the step above first.</p>}
    </div>
  );
}

function StepConfigureProvider({
  done,
  creds,
  onChange,
}: {
  done: boolean;
  creds: ProviderCredential[];
  onChange: () => void;
}) {
  const active = creds.filter((c) => c.status === "active");
  return (
    <StepShell index={1} title="Configure a provider" done={done}>
      <p className="mb-3 text-sm text-slate-500">
        Add at least one provider credential — encrypted at rest automatically, no environment
        variables to set. You can add more, or change these, later from Settings → Providers.
      </p>
      <div className="space-y-3">
        {PROVIDERS.map((p) => {
          const cred = active.find((c) => c.provider === p.value);
          return (
            <ProviderRow key={p.value} provider={p.value} label={p.label} note={p.note} credential={cred} onChange={onChange} />
          );
        })}
      </div>
    </StepShell>
  );
}

function StepCreateApplication({
  done,
  app,
  unlocked,
  onCreated,
}: {
  done: boolean;
  app?: Application;
  unlocked: boolean;
  onCreated: () => void;
}) {
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setCreating(true);
    setError(null);
    try {
      await api.createApplication({ name });
      setName("");
      onCreated();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to create application.");
    } finally {
      setCreating(false);
    }
  }

  return (
    <StepShell index={2} title="Create your first application" done={done} unlocked={unlocked}>
      {app ? (
        <p className="text-sm text-slate-600">
          <span className="font-medium text-slate-900">{app.name}</span> is created — proceed to the next step to connect it.
        </p>
      ) : (
        <form onSubmit={handleSubmit} className="flex items-end gap-3">
          <div className="flex-1">
            <Field label="Application name">
              <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder="Document AI" required />
            </Field>
          </div>
          <div className="pb-4">
            <Button type="submit" disabled={creating}>
              {creating ? "Creating…" : "Create"}
            </Button>
          </div>
        </form>
      )}
      <ErrorBanner message={error} />
    </StepShell>
  );
}

function StepConnect({ app, unlocked }: { app?: Application; unlocked: boolean }) {
  const [key, setKey] = useState<ApiKey | null>(null);
  const [issuing, setIssuing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const router = useRouter();

  async function handleGenerateKey() {
    if (!app) return;
    setIssuing(true);
    setError(null);
    try {
      const created = await api.createKey(app.id, { name: "default" });
      setKey(created);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to create API key.");
    } finally {
      setIssuing(false);
    }
  }

  return (
    <StepShell index={3} title="Connect it" done={!!key} unlocked={unlocked}>
      <p className="mb-3 text-sm text-slate-500">
        Generate an API key, then point your existing OpenAI-compatible client at Fluxen by
        changing only its base URL and API key.
      </p>

      <ErrorBanner message={error} />

      {!key && (
        <Button onClick={handleGenerateKey} disabled={issuing}>
          {issuing ? "Generating…" : "Generate API key"}
        </Button>
      )}

      {key && (
        <div className="space-y-4">
          <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800">
            This key is shown only once. Copy it now — Fluxen never displays it again.
          </div>
          <div>
            <p className="mb-1 text-sm font-medium text-slate-700">Your API key</p>
            <CodeBlock>{key.key}</CodeBlock>
          </div>
          <div>
            <p className="mb-1 text-sm font-medium text-slate-700">Python (openai SDK)</p>
            <CodeBlock>{pythonSnippet(key.key)}</CodeBlock>
          </div>
          <Button
            onClick={() => router.replace(app ? `/applications/${app.id}` : "/applications")}
          >
            Done — go to my application
          </Button>
        </div>
      )}
    </StepShell>
  );
}

function pythonSnippet(key: string): string {
  return `from openai import OpenAI

client = OpenAI(
    base_url="${GATEWAY_BASE_URL}",
    api_key="${key}",
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Hello, Fluxen!"}],
)
print(response.choices[0].message.content)`;
}
