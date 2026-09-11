"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError } from "@/lib/api";
import { Button, Card, ErrorBanner, Field, PageTitle, Subtitle, inputClass } from "@/components/ui";

// The first-run setup wizard (Part J): create the organization and its
// owner account. Only reachable while no organization exists yet — the
// backend rejects a second call with 409, and the root page never routes
// here once setup is complete.
export default function SetupPage() {
  const router = useRouter();
  const [orgName, setOrgName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await api.setup({ org_name: orgName, email, password });
      router.replace("/applications");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Setup failed. Please try again.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center p-8">
      <Card>
        <PageTitle>Set up Fluxen</PageTitle>
        <Subtitle>Create your organization and owner account.</Subtitle>

        <ErrorBanner message={error} />

        <form onSubmit={handleSubmit}>
          <Field label="Organization name">
            <input
              className={inputClass}
              value={orgName}
              onChange={(e) => setOrgName(e.target.value)}
              placeholder="Acme"
              required
            />
          </Field>
          <Field label="Email">
            <input
              className={inputClass}
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              required
            />
          </Field>
          <Field label="Password">
            <input
              className={inputClass}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="At least 8 characters"
              minLength={8}
              required
            />
          </Field>

          <Button type="submit" disabled={submitting}>
            {submitting ? "Creating…" : "Create owner account"}
          </Button>
        </form>
      </Card>
    </main>
  );
}
