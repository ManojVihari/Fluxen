import { PageHero, Section, Card, Grid, Cell } from "@/components/ui";

export default function WhyFluxenPage() {
  return (
    <main>
      <PageHero
        eyebrow="Why Fluxen"
        title="Fluxen isn't another AI gateway."
        subtitle="It's the layer that sits on top of one: the part that tells you what to change, proves it against your own traffic before you touch production, and confirms afterward that it actually worked."
      />

      <Section title="The market has three kinds of tools today — and a gap between them">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Card title="Gateways & routers">
            Proxies like LiteLLM, Portkey, Cloudflare AI Gateway, and Kong give you one endpoint across
            providers, plus fallbacks, load balancing, caching, and budgets. They&apos;re the plumbing — real,
            necessary, and increasingly commoditized. None of them tell you which lever to pull.
          </Card>
          <Card title="Observability tools">
            Helicone and similar tools log every request and show you cost, latency, and errors across
            providers. Excellent for seeing what happened. They stop at the dashboard — they don&apos;t turn a
            spend spike into a specific, provable recommendation.
          </Card>
          <Card title="AI FinOps / reporting">
            A newer category (Vantage, nOps, Finout, and similar) brings cloud-style cost allocation and
            chargeback to AI spend, for finance and platform teams. They report on spend after the fact — they
            don&apos;t sit in the request path and can&apos;t apply or enforce a fix themselves.
          </Card>
        </div>
        <p className="mt-6 max-w-2xl text-sm text-slate-600">
          All three categories answer some version of <em>&quot;what is my AI traffic doing.&quot;</em> None of
          them close the loop to <em>&quot;here is exactly what to change, proof it will work, and confirmation
          that it did.&quot;</em> That's the gap Fluxen is built to fill — not a better proxy, not a prettier
          dashboard, but the decision layer none of the above are trying to be.
        </p>
      </Section>

      <Section title="How the closed loop compares">
        <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200 text-left text-xs uppercase tracking-wide text-slate-500">
                <th className="px-4 py-3 font-medium">Capability</th>
                <th className="px-4 py-3 text-center font-medium">Gateways & routers</th>
                <th className="px-4 py-3 text-center font-medium">Observability tools</th>
                <th className="px-4 py-3 text-center font-medium">AI FinOps / reporting</th>
                <th className="px-4 py-3 text-center font-medium text-slate-900">Fluxen</th>
              </tr>
            </thead>
            <tbody className="text-slate-700">
              <Row label="Unified endpoint across providers">
                <Cell state="yes" /><Cell state="no" /><Cell state="no" /><Cell state="yes" />
              </Row>
              <Row label="Routing, caching, budgets, rate limits">
                <Cell state="yes" /><Cell state="no" /><Cell state="no" /><Cell state="yes" />
              </Row>
              <Row label="Cost & usage visibility per app">
                <Cell state="partial" /><Cell state="yes" /><Cell state="yes" /><Cell state="yes" />
              </Row>
              <Row label="Auto-detected opportunities with evidence">
                <Cell state="no" /><Cell state="no" /><Cell state="no" /><Cell state="yes" />
              </Row>
              <Row label="Simulates a change against real history first">
                <Cell state="no" /><Cell state="no" /><Cell state="no" /><Cell state="yes" />
              </Row>
              <Row label="Applies the fix as a real, enforced control">
                <Cell state="partial" /><Cell state="no" /><Cell state="no" /><Cell state="yes" />
              </Row>
              <Row label="Measures the real outcome after, with a verdict">
                <Cell state="no" /><Cell state="no" /><Cell state="no" /><Cell state="yes" />
              </Row>
              <Row label="One-click revert on a bad outcome">
                <Cell state="no" /><Cell state="no" /><Cell state="no" /><Cell state="yes" />
              </Row>
            </tbody>
          </table>
        </div>
        <p className="mt-4 text-xs text-slate-400">
          Categories, not individual products — capabilities vary by vendor and change fast. ✓ = built-in and
          central to the product. ◐ = present in some form but not the specific shape described. This is our
          own read of the public positioning of each category as of when this page was written, not a claim
          about any single named competitor's roadmap.
        </p>
      </Section>

      <Section title="The category is consolidating. Staying independent is a feature, not a gap.">
        <p className="max-w-2xl text-sm text-slate-600">
          Two of the best-known names in this exact space changed hands in 2026: Portkey was acquired by Palo
          Alto Networks and folded into its Prisma AIRS agent-security platform — its center of gravity is now
          agent governance and security, not cost efficiency. Helicone was acquired by Mintlify and has moved
          into maintenance mode. Neither outcome is a knock on either team — it's just what happens when a
          gateway becomes a feature of someone else's platform instead of staying the product. Fluxen is
          self-hosted, single-purpose, and not for sale as a feature of anything else: your traffic, your
          Postgres, your infrastructure, no vendor roadmap deciding what happens to the tool next.
        </p>
      </Section>

      <Section title="The application is the unit of optimization">
        <p className="mb-4 max-w-2xl text-sm text-slate-600">
          Instead of showing you cost by model or cost by provider, Fluxen builds an Efficiency Profile for
          every application — a single score, the issues behind it, and the actions that would move it.
        </p>
        <div className="mx-auto max-w-md rounded-lg border border-slate-200 bg-slate-50 p-4 font-mono text-xs text-slate-700">
          <p>DOCUMENT-AI</p>
          <p className="mt-2">Spend&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;$1,240</p>
          <p>Requests&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;182,421</p>
          <p>Efficiency Score&nbsp;&nbsp;72/100</p>
          <p className="mt-2">Issues found:</p>
          <p>• Model cost opportunity</p>
          <p>• Repeated requests</p>
          <p>• Token growth</p>
        </div>
      </Section>

      <Section title="No single feature here is revolutionary. The loop is.">
        <Grid>
          <Card title="Evidence before action">
            Every opportunity shows its sample size, eligible traffic, and confidence — before it asks you to
            do anything.
          </Card>
          <Card title="Simulation before production">
            Every recommendation is replayed against real historical traffic first, using the exact pricing and
            caching logic production uses. Nothing changes production until you've seen the actual delta.
          </Card>
          <Card title="Measured, not assumed">
            Every applied change gets a real before/after verdict, 7 and 14 days later — successful, partial,
            no effect, or regressed — not just a projection.
          </Card>
        </Grid>
      </Section>

      <Section title="A product focus, not a checklist">
        <p className="max-w-2xl text-sm text-slate-600">
          Fluxen doesn&apos;t chase every feature a competitor ships. We're not trying to out-route LiteLLM,
          out-log Helicone, or out-report a FinOps platform. Fluxen wins by being exceptionally good at one
          thing none of them are built to do: turning &quot;here's your AI traffic&quot; into a specific,
          proven, measured decision — honestly, with evidence, and without ever changing production without
          your confirmation.
        </p>
      </Section>
    </main>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <tr className="border-b border-slate-100 last:border-0">
      <td className="px-4 py-3">{label}</td>
      {Array.isArray(children)
        ? children.map((c, i) => (
            <td key={i} className="px-4 py-3 text-center">
              {c}
            </td>
          ))
        : children}
    </tr>
  );
}
