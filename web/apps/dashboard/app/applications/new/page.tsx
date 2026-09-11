"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError } from "@/lib/api";
import { Button, Card, ErrorBanner, Field, PageTitle, Subtitle, inputClass } from "@/components/ui";

export default function NewApplicationPage() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const app = await api.createApplication({ name });
      router.replace(`/applications/${app.id}/connect`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        router.replace("/login");
        return;
      }
      setError(err instanceof ApiError ? err.message : "Failed to create application.");
      setSubmitting(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center p-8">
      <Card>
        <PageTitle>New application</PageTitle>
        <Subtitle>
          An application is the unit Fluxen optimizes — one API key, one traffic
          stream, one efficiency profile.
        </Subtitle>

        <ErrorBanner message={error} />

        <form onSubmit={handleSubmit}>
          <Field label="Name">
            <input
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Document AI"
              autoFocus
              required
            />
          </Field>

          <Button type="submit" disabled={submitting}>
            {submitting ? "Creating…" : "Create application"}
          </Button>
        </form>
      </Card>
    </main>
  );
}
