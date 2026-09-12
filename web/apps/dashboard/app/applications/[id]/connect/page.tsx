"use client";

import { useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { api, ApiError, GATEWAY_BASE_URL, type ApiKey, type ProviderCredential } from "@/lib/api";
import { Button, CodeBlock, ErrorBanner } from "@/components/ui";
import { PROVIDERS, ProviderRow } from "@/components/provider-setup";

// The connect screen: issue an API key, show it exactly once, and give the
// user copy-paste snippets to point an OpenAI-compatible client at Fluxen
// (Part L Phase 1: "connect screen shows the exact base_url + key + a
// copy-paste snippet"). This is the screen the Aha-Moment journey (Part J)
// depends on every earlier phase to reach. The application header and tab
// nav are provided by the shared Application Detail layout.
export default function ConnectPage() {
  const params = useParams<{ id: string }>();

  const [key, setKey] = useState<ApiKey | null>(null);
  const [revoked, setRevoked] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [issuing, setIssuing] = useState(false);
  const [creds, setCreds] = useState<ProviderCredential[] | null>(null);

  function loadCreds() {
    api.listProviderCredentials().then(setCreds).catch(() => {});
  }

  useEffect(() => {
    loadCreds();
  }, []);

  const activeCreds = creds ?? [];
  const providerConfigured = activeCreds.some((c) => c.status === "active");

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

  return (
    <div>
      <p className="mb-6 text-sm text-slate-500">
        Point your existing OpenAI-compatible client at Fluxen by changing its
        base URL and API key — nothing else about your application needs to
        change.
      </p>

      <ErrorBanner message={error} />

      {creds !== null && !providerConfigured && (
        <div className="mb-6 space-y-3">
          <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800">
            No provider is configured for this organization yet — requests through this key will
            fail with <code className="rounded bg-amber-100 px-1">503 no_provider_credential</code>{" "}
            until one is added. Add one below, or later from Settings → Providers.
          </div>
          <div className="space-y-3">
            {PROVIDERS.map((p) => {
              const cred = activeCreds.find((c) => c.status === "active" && c.provider === p.value);
              return (
                <ProviderRow key={p.value} provider={p.value} label={p.label} note={p.note} credential={cred} onChange={loadCreds} />
              );
            })}
          </div>
        </div>
      )}

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
    </div>
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
