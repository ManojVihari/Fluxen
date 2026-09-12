import { PageHero, Section, Card, Grid } from "@/components/ui";

export default function ProductPage() {
  return (
    <main>
      <PageHero
        eyebrow="Product"
        title="Everything you need to run AI traffic efficiently"
        subtitle="One self-hosted deployment: gateway, application intelligence, cost intelligence, four detectors, simulation, control, and measurement."
      />

      <Section title="Gateway">
        <p className="mb-4 max-w-2xl text-sm text-slate-600">
          A single OpenAI-compatible ingress in front of OpenAI, Google Gemini, and Ollama. Application-scoped
          API keys, streaming and non-streaming, usage extraction, integer micro-USD cost calculation, exact
          caching, rate limiting, budgets, and percentage-based routing.
        </p>
      </Section>

      <Section title="Application Efficiency Profile">
        <p className="mb-4 max-w-2xl text-sm text-slate-600">
          Applications, not raw requests, are the unit of optimization in Fluxen. Every application gets an
          Efficiency Score — five weighted components (model, token, cache, traffic stability, cost
          efficiency) — plus its own opportunities, policies, and request history.
        </p>
      </Section>

      <Section title="Four detectors">
        <Grid cols={2}>
          <Card title="Model cost">
            Finds traffic on an expensive model that a cheaper, catalog-declared alternative could plausibly
            handle, based on real token-size and feature-profile evidence.
          </Card>
          <Card title="Repeated request">
            Finds exact-duplicate request traffic that a TTL cache would have served for free, with a savings
            curve across four candidate TTLs.
          </Card>
          <Card title="Token efficiency">
            Finds sustained drift in input or output token usage per request, with a change-point estimate for
            when the drift began.
          </Card>
          <Card title="Traffic anomaly">
            Finds statistically unusual request volume, cost, token usage, or error rate against an
            application's own seasonal baseline — a stability signal, not a savings claim.
          </Card>
        </Grid>
      </Section>

      <Section title="Simulate, control, measure">
        <p className="max-w-2xl text-sm text-slate-600">
          Every recommendation can be replayed against real historical traffic before anything changes
          production. Applying a change is one human-confirmed action across five controls — routing, caching,
          budgets, rate limits, model restrictions. Every applied change gets an honest before/after verdict,
          with revert always one click away.
        </p>
      </Section>
    </main>
  );
}
