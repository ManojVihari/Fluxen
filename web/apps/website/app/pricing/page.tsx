import { PageHero, Section, Card } from "@/components/ui";

export default function PricingPage() {
  return (
    <main>
      <PageHero
        eyebrow="Pricing"
        title="Free, open-source, and self-hosted"
        subtitle="V1 is about adoption and validation, not monetization. Run Fluxen on your own infrastructure at no cost, against your own provider bills."
      />

      <Section>
        <Card title="Self-hosted — $0">
          <p className="mb-3">
            The full product: gateway, four detectors, simulation, control, measurement, and dashboard. Your
            own Postgres and Redis, your own provider credentials, your own data — nothing leaves your
            infrastructure.
          </p>
          <p>
            You pay OpenAI, Google, or your own hardware for the AI traffic itself, exactly as you would
            without Fluxen in the path. Fluxen's job is making that spend go further, not adding to it.
          </p>
        </Card>
      </Section>

      <Section title="What's next">
        <p className="max-w-2xl text-sm text-slate-600">
          A future hosted "Managed Fluxen" offering and enterprise capabilities (SSO, RBAC, audit logs,
          advanced governance) are potential future commercial directions — not part of V1, and not required
          to get real value from the self-hosted product today.
        </p>
      </Section>
    </main>
  );
}
