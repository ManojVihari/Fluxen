import Link from "next/link";
import { PageHero, Section, Card, Grid } from "@/components/ui";

// Home (Part I of the PRD's public-website spec, Implementation Plan
// Part L Phase 7). The gateway (proxy, routing, caching, budgets) is
// real but deliberately not the headline here — that layer is
// increasingly commoditized across the market (LiteLLM, Portkey,
// Cloudflare, Kong all do some version of it). The headline is the part
// none of them are built to do: turn traffic into a proven, measured
// decision. See /why-fluxen for the full, researched case.
export default function Home() {
  return (
    <main>
      <PageHero
        eyebrow="Not another AI gateway"
        title="Your AI traffic isn't short on gateways. It's short on decisions."
        subtitle="Fluxen proxies OpenAI, Gemini, and Ollama traffic too — but that's the substrate, not the product. The product is finding what's inefficient, proving the fix against your real history, applying it safely, and confirming it actually worked."
      />

      <div className="mx-auto flex max-w-3xl justify-center gap-3 pb-4">
        <Link
          href="/docs"
          className="rounded-md bg-slate-900 px-5 py-2.5 text-sm font-medium text-white hover:bg-slate-700"
        >
          Get started
        </Link>
        <Link
          href="/product"
          className="rounded-md border border-slate-300 px-5 py-2.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
        >
          See the product
        </Link>
      </div>
      <p className="mb-12 text-center text-sm text-slate-500">
        Wondering how this is different from LiteLLM, Portkey, or a FinOps dashboard?{" "}
        <Link href="/why-fluxen" className="font-medium text-slate-700 underline underline-offset-2 hover:text-slate-900">
          See the comparison
        </Link>
        .
      </p>

      <Section title="The core loop">
        <Grid>
          <Card title="1. Understand">
            Every request through the gateway is accounted for — cost, tokens, model, provider, latency, cache
            status. No sampling, no estimation.
          </Card>
          <Card title="2. Identify">
            Four detectors continuously look for real, evidence-backed inefficiency: model cost, repeated
            requests, token drift, and traffic anomalies.
          </Card>
          <Card title="3. Simulate">
            Before anything changes, Fluxen replays the scenario against your real historical traffic and shows
            the actual cost delta — not a guess.
          </Card>
        </Grid>
        <div className="mt-4">
          <Grid cols={2}>
            <Card title="4. Control">
              Apply a proven change with one human-confirmed action: percentage-based model routing, exact
              caching, budgets, rate limits, or model restrictions.
            </Card>
            <Card title="5. Measure">
              Every applied change gets a before/after verdict against real traffic 7 and 14 days later —
              successful, partial, no effect, or regressed, with one-click revert.
            </Card>
          </Grid>
        </div>
      </Section>

      <Section title="What makes this different">
        <Grid>
          <Card title="Evidence before action">
            Every opportunity shows its evidence — sample size, eligible traffic, confidence — before you're
            asked to do anything.
          </Card>
          <Card title="Simulation before production">
            Nothing changes production traffic until you've seen the real, replayed impact against your own
            history.
          </Card>
          <Card title="Measured, not assumed">
            Fluxen tells you what actually happened after a change, not just what it estimated beforehand.
          </Card>
        </Grid>
      </Section>

      <Section title="Three providers, one gateway">
        <p className="max-w-2xl text-sm text-slate-600">
          OpenAI, Google Gemini, and Ollama — behind one OpenAI-compatible endpoint. Point your existing client
          at Fluxen and route, cache, and control traffic across all three without touching application code.
        </p>
      </Section>
    </main>
  );
}
