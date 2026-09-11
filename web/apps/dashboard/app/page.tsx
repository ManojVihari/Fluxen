// Phase 0 placeholder. The real setup wizard, login, and authenticated
// dashboard shell are built in Phase 1 ("Authentication & Applications") —
// this page exists only to prove the Next.js app builds and serves.
export default function Home() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-2 p-8">
      <h1 className="text-2xl font-semibold">Fluxen</h1>
      <p className="text-slate-500">Dashboard — foundation phase.</p>
    </main>
  );
}
