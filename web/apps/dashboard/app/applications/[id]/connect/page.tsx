"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api, ApiError, GATEWAY_BASE_URL, type Application, type ApiKey } from "@/lib/api";
import { Button, CodeBlock, ErrorBanner } from "@/components/ui";

// The connect screen: issue an API key, show it exactly once, and give the
// user copy-paste snippets to point an OpenAI-compatible client at Fluxen
// (Part L Phase 1: "connect screen shows the exact base_url + key + a
// copy-paste snippet"). This is the screen the Aha-Moment journey (Part J)
// depends on every earlier phase to reach.
export default function ConnectPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [app, setApp] = useState<Application | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [key, setKey] = useState<ApiKey | null>(null);
  const [revoked, setRevoked] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [issuing, setIssuing] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api
      .listApplications()
      .then((apps) => {
        if (cancelled) return;
        const found = apps.find((a) => a.id === params.id) ?? null;
        if (!found) {
          setNotFound(true);
          return;
        }
        setApp(found);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load application.");
      });
    return () => {
      cancelled = true;
    };
  }, [params.id, router]);

  async function handleGenerateKey() {
    setError(null);
    setIssuing(true);
    try {
      const created = await api.createKey(params.id, { name: "default" });
      setKey(created);
      setRevoked(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to create API key.");
    } finally {
      setIssuing(false);
    }
  }

  async function handleRevoke() {
    if (!key) return;
    setError(null);
    try {
      await api.revokeKey(key.id);
      setRevoked(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to revoke API key.");
    }
  }

  if (notFound) {
    return (
      <main className="mx-auto max-w-2xl p-8">
        <ErrorBanner message="Application not found." />
        <Link href="/applications" className="text-sm underline">
          Back to applications
        </Link>
      </main>
    );
  }

  return (
    <main className="mx-auto max-w-2xl p-8">
      <Link href="/applications" className="text-sm text-slate-500 underline-offset-2 hover:underline">
        ← Applications
      </Link>

      <h1 className="mb-1 mt-2 text-xl font-semibold text-slate-900">
        {app ? `Connect ${app.name}` : "Connect application"}
      </h1>
      <p className="mb-6 text-sm text-slate-500">
        Point your existing OpenAI-compatible client at Fluxen by changing its
        base URL and API key — nothing else about your application needs to
        change.
      </p>

      <ErrorBanner message={error} />

      {!key && (
        <Button onClick={handleGenerateKey} disabled={issuing}>
          {issuing ? "Generating…" : "Generate API key"}
        </Button>
      )}

      {key && !revoked && (
        <div className="space-y-4">
          <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800">
            This key is shown only once. Copy it now — Fluxen never displays
            it again.
          </div>

          <div>
            <p className="mb-1 text-sm font-medium text-slate-700">Your API key</p>
            <CodeBlock>{key.key}</CodeBlock>
          </div>

          <div>
            <p className="mb-1 text-sm font-medium text-slate-700">Python (openai SDK)</p>
            <CodeBlock>{pythonSnippet(key.key)}</CodeBlock>
          </div>

          <div>
            <p className="mb-1 text-sm font-medium text-slate-700">Node (openai SDK)</p>
            <CodeBlock>{nodeSnippet(key.key)}</CodeBlock>
          </div>

          <div>
            <p className="mb-1 text-sm font-medium text-slate-700">curl</p>
            <CodeBlock>{curlSnippet(key.key)}</CodeBlock>
          </div>

          <Button variant="danger" onClick={handleRevoke}>
            Revoke this key
          </Button>
        </div>
      )}

      {revoked && (
        <div className="space-y-4">
          <div className="rounded-md border border-slate-300 bg-slate-50 px-3 py-2 text-sm text-slate-700">
            This key has been revoked and can no longer be used.
          </div>
          <Button onClick={handleGenerateKey} disabled={issuing}>
            {issuing ? "Generating…" : "Generate a new key"}
          </Button>
        </div>
      )}
    </main>
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

function nodeSnippet(key: string): string {
  return `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${GATEWAY_BASE_URL}",
  apiKey: "${key}",
});

const response = await client.chat.completions.create({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Hello, Fluxen!" }],
});
console.log(response.choices[0].message.content);`;
}

function curlSnippet(key: string): string {
  return `curl ${GATEWAY_BASE_URL}/chat/completions \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello, Fluxen!"}]
  }'`;
}
