import Link from "next/link";
import { PageHero, Section, Card, Grid, CodeBlock, Step, Button } from "@/components/ui";

const REPO = "https://github.com/ManojVihari/Fluxen";

export default function DocsPage() {
  return (
    <main>
      <PageHero
        eyebrow="Docs"
        title="Running in under five minutes"
        subtitle="Clone the repo, run one command, and reach a measured, revertible optimization outcome — no configuration required to start."
      />

      <Section kicker="Get the code" title="1. Clone the repository">
        <CodeBlock label="terminal">{`git clone ${REPO}.git
cd Fluxen`}</CodeBlock>
      </Section>

      <Section kicker="Run it" title="2. Deploy with Docker">
        <p className="mb-6 max-w-2xl text-sm text-slate-600">
          One command brings up all four services — Postgres, Redis, the combined gateway/control binary, and the
          dashboard. Every setting has a working default: no <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">.env</code> file
          to create, no API key to set before it boots, no encryption key to generate by hand.
        </p>
        <CodeBlock label="terminal">{`docker compose up --build`}</CodeBlock>
        <p className="mt-4 text-sm text-slate-500">
          Prefer running the Go/Node processes yourself for local development instead of Docker? See{" "}
          <a href={`${REPO}#run-locally-without-docker`} className="font-medium text-slate-700 underline underline-offset-2 hover:text-slate-900">
            Run locally, without Docker
          </a>{" "}
          in the README.
        </p>
      </Section>

      <Section kicker="First run" title="3. Complete the setup wizard">
        <div className="space-y-5">
          <Step n={1} title="Open the dashboard">
            Visit <a href="http://localhost:3000" className="font-medium text-slate-700 underline underline-offset-2 hover:text-slate-900">localhost:3000</a>.
            On a fresh deployment you land on the one-time setup wizard.
          </Step>
          <Step n={2} title="Create your organization and owner account">
            This runs exactly once per deployment — see{" "}
            <Link href="/product" className="font-medium text-slate-700 underline underline-offset-2 hover:text-slate-900">the product overview</Link>{" "}
            for how Fluxen's single-tenant-per-deployment model works. Add teammates later from Settings → Users.
          </Step>
          <Step n={3} title="Add a provider credential">
            From Settings → Providers, add an OpenAI, Gemini, or self-hosted Ollama credential. It's encrypted at
            rest with a key Fluxen generates for itself — nothing to configure.
          </Step>
        </div>
      </Section>

      <Section kicker="Connect your app" title="4. Point your existing client at Fluxen">
        <div className="space-y-5">
          <Step n={1} title="Create an application and generate a key">
            Applications are Fluxen's unit of optimization. Create one, then click Generate API key on the
            connect screen — the raw key is shown exactly once.
          </Step>
          <Step n={2} title="Change two lines in your code">
            Nothing else about your application needs to change.
          </Step>
        </div>
        <div className="mt-4">
          <CodeBlock label="python">{`from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="fx_live_...",
)
client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Hello, Fluxen!"}],
)`}</CodeBlock>
        </div>
      </Section>

      <Section kicker="See it work" title="5. Watch traffic and opportunities appear">
        <p className="max-w-2xl text-sm text-slate-600">
          Usage, cost, and model mix show up in Application Detail within a few minutes. Once traffic clears the
          minimum volume floor, detectors start surfacing real, evidence-backed opportunities in Optimizations.
          Want to see it immediately instead of waiting for real traffic? Run{" "}
          <code className="rounded bg-slate-100 px-1.5 py-0.5 text-xs">docker compose exec fluxen fluxenctl seed --demo</code>{" "}
          to seed a realistic demo application and see the whole loop end to end.
        </p>
      </Section>

      <Section kicker="Reference" title="Guides">
        <Grid>
          <Card title="Self-hosting guide">
            Deployment topology, environment variables (all optional), security notes, and the load-testing
            methodology — <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/self-hosting.md</code> in the repo.
          </Card>
          <Card title="Concepts">
            Applications, opportunities, policies, the Efficiency Score formula, and value labeling
            (estimated/projected/measured/realized) — <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/concepts.md</code>.
          </Card>
          <Card title="Provider setup">
            Configuring OpenAI, Gemini, and Ollama credentials, including the encrypted Settings → Providers flow
            — <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/providers.md</code>.
          </Card>
        </Grid>
      </Section>

      <Section kicker="Reference" title="API reference">
        <p className="mb-6 max-w-2xl text-sm text-slate-600">
          A hand-maintained reference for the control API lives in{" "}
          <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/api-reference.md</code> in the repository. An
          OpenAPI-generated reference is on the roadmap but not yet built.
        </p>
        <Button href={REPO} variant="secondary">View source on GitHub</Button>
      </Section>
    </main>
  );
}
