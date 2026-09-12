import Link from "next/link";

const LINKS = [
  { href: "/product", label: "Product" },
  { href: "/how-it-works", label: "How it Works" },
  { href: "/why-fluxen", label: "Why Fluxen" },
  { href: "/providers", label: "Providers" },
  { href: "/pricing", label: "Pricing" },
  { href: "/docs", label: "Docs" },
];

export function Nav() {
  return (
    <header className="border-b border-slate-200">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-6 py-4">
        <Link href="/" className="text-sm font-semibold text-slate-900">
          Fluxen
        </Link>
        <nav className="hidden gap-6 sm:flex">
          {LINKS.map((l) => (
            <Link key={l.href} href={l.href} className="text-sm text-slate-600 hover:text-slate-900">
              {l.label}
            </Link>
          ))}
        </nav>
        <Link
          href="/docs"
          className="rounded-md bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-700"
        >
          Get started
        </Link>
      </div>
    </header>
  );
}

export function Footer() {
  return (
    <footer className="border-t border-slate-200 py-10">
      <div className="mx-auto max-w-5xl px-6 text-sm text-slate-500">
        <p>Fluxen — AI Traffic, Optimized. Self-hosted, open, and free of vendor lock-in.</p>
        <div className="mt-4 flex flex-wrap gap-x-6 gap-y-2">
          <Link href="/product" className="hover:text-slate-800">Product</Link>
          <Link href="/how-it-works" className="hover:text-slate-800">How it Works</Link>
          <Link href="/why-fluxen" className="hover:text-slate-800">Why Fluxen</Link>
          <Link href="/providers" className="hover:text-slate-800">Providers</Link>
          <Link href="/pricing" className="hover:text-slate-800">Pricing</Link>
          <Link href="/docs" className="hover:text-slate-800">Docs</Link>
        </div>
      </div>
    </footer>
  );
}
