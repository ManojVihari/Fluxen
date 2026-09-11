// Phase 0 placeholder. The real public site (Home, Product, How it Works,
// Why Fluxen, Providers, Pricing, Docs) is built in Phase 7
// ("Complete V1 Product") — this page exists only to prove the Next.js
// app builds and statically exports.
export default function Home() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-2 p-8">
      <h1 className="text-2xl font-semibold">Fluxen</h1>
      <p className="text-slate-500">AI Traffic, Optimized. — coming soon.</p>
    </main>
  );
}
