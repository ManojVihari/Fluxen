import { PageHero, Section, Card } from "@/components/ui";

export default function HowItWorksPage() {
  return (
    <main>
      <PageHero
        eyebrow="How it works"
        title="Point your existing client at Fluxen"
        subtitle="No SDK changes. Fluxen is OpenAI-compatible — swap the base URL and API key, and every request starts flowing through the gateway."
      />

      <Section>
        <ol className="mx-auto max-w-2xl space-y-6">
          <Step n={1} title="Connect an application">
            Create an application in the dashboard, issue an API key, and point your client's{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">base_url</code> at your Fluxen
            deployment. Traffic starts appearing within seconds.
          </Step>
          <Step n={2} title="Understand">
            Fluxen accounts for every request — cost, tokens, model, provider, latency, cache status — no
            sampling. Application Detail shows spend, model mix, and trends immediately.
          </Step>
          <Step n={3} title="Identify">
            Once an application clears a minimum traffic/spend floor, four detectors run against its real
            history and surface opportunities with evidence, confidence, and an estimated dollar impact.
          </Step>
          <Step n={4} title="Simulate">
            Before anything changes, replay the recommended scenario against real historical requests and see
            the actual delta — not an estimate extrapolated from a formula.
          </Step>
          <Step n={5} title="Control">
            Apply the change with one confirmed action. Fluxen writes a versioned policy and enforces it on the
            gateway's hot path immediately.
          </Step>
          <Step n={6} title="Measure">
            7 and 14 days later, Fluxen checks what actually happened against real traffic and gives an honest
            verdict — successful, partial, no effect, or regressed. A regression is one click from reverted.
          </Step>
        </ol>
      </Section>

      <Section title="Deployment">
        <Card title="docker compose up">
          The entire product — gateway, control API, background jobs, dashboard, Postgres, Redis — runs from
          one repository with one command. No message broker, no Kubernetes, no managed service required.
        </Card>
      </Section>
    </main>
  );
}

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <li className="flex gap-4">
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-slate-900 text-sm font-semibold text-white">
        {n}
      </span>
      <div>
        <p className="text-sm font-semibold text-slate-900">{title}</p>
        <p className="mt-1 text-sm text-slate-600">{children}</p>
      </div>
    </li>
  );
}
