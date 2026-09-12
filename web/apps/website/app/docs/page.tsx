import { PageHero, Section, Card, Grid } from "@/components/ui";

export default function DocsPage() {
  return (
    <main>
      <PageHero
        eyebrow="Docs"
        title="Get started in about 15 minutes"
        subtitle="Clone the repo, run one command, and reach a measured, revertible optimization outcome."
      />

      <Section title="Quickstart">
        <ol className="mx-auto max-w-2xl list-decimal space-y-3 pl-5 text-sm text-slate-700">
          <li>
            Clone the repository and copy <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">.env.example</code> to{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">.env</code>. Set at least{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">OPENAI_API_KEY</code> (Gemini and Ollama
            are optional and can be added later from Settings → Providers).
          </li>
          <li>
            Run <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docker compose up</code>. Postgres,
            Redis, the combined gateway/control binary, and the dashboard all start together.
          </li>
          <li>
            Open the dashboard, complete the one-time setup wizard (organization + owner account), and create
            your first application.
          </li>
          <li>
            Copy the application's API key and connection snippet, point your existing OpenAI client's{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">base_url</code> at your Fluxen
            deployment, and send a real request.
          </li>
          <li>
            Traffic shows up in Application Detail within seconds. Once the application clears the minimum
            traffic/spend floor, detectors begin surfacing opportunities in Optimizations.
          </li>
        </ol>
      </Section>

      <Section title="Guides">
        <Grid>
          <Card title="Self-hosting guide">
            Deployment topology, environment variables, and the production Docker images — see{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/self-hosting.md</code> in the
            repository.
          </Card>
          <Card title="Concepts">
            Applications, opportunities, policies, the Efficiency Score formula, and value labeling
            (estimated/projected/measured/realized) — see{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/concepts.md</code>.
          </Card>
          <Card title="Provider setup">
            Configuring OpenAI, Gemini, and Ollama credentials, including the encrypted Settings → Providers
            flow — see <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/providers.md</code>.
          </Card>
        </Grid>
      </Section>

      <Section title="API reference">
        <p className="max-w-2xl text-sm text-slate-600">
          A hand-maintained reference for the control API lives at{" "}
          <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">docs/api-reference.md</code> in the
          repository. An OpenAPI-generated reference is on the roadmap but not yet built.
        </p>
      </Section>
    </main>
  );
}
