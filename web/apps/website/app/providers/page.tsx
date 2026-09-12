import { PageHero, Section, Card, Grid } from "@/components/ui";

export default function ProvidersPage() {
  return (
    <main>
      <PageHero
        eyebrow="Providers"
        title="Exactly three providers, fully supported"
        subtitle="OpenAI, Google Gemini, and Ollama — behind one OpenAI-compatible endpoint. No half-supported long tail of 100+ providers to maintain."
      />

      <Section>
        <Grid>
          <Card title="OpenAI">
            Full support: chat (streaming and non-streaming), provider-reported usage, tools/function calling,
            vision, JSON/schema response format, and the live <code>/v1/models</code> catalog.
          </Card>
          <Card title="Google Gemini">
            Full support via translation: the same request/response dialect your client already speaks is
            translated to and from Gemini's native API, including function calling and safety-block handling.
          </Card>
          <Card title="Ollama">
            Full support for self-hosted models via Ollama's native API, including live model discovery. Ollama
            traffic is always labeled "Local · not billed" — Fluxen never fabricates a cost for it.
          </Card>
        </Grid>
      </Section>

      <Section title="One gateway, real routing">
        <p className="max-w-2xl text-sm text-slate-600">
          Percentage-based model routing, exact caching, budgets, and rate limits work the same way across all
          three providers. A model-cost opportunity can recommend rerouting a share of traffic from one
          provider's model to a cheaper one on the same or a different provider — simulated against real
          history before you apply it.
        </p>
      </Section>
    </main>
  );
}
