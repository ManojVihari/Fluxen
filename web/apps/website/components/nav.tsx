import Link from "next/link";

const GITHUB_URL = "https://github.com/ManojVihari/Fluxen";

const LINKS = [
  { href: "/product", label: "Product" },
  { href: "/how-it-works", label: "How it Works" },
  { href: "/why-fluxen", label: "Why Fluxen" },
  { href: "/providers", label: "Providers" },
  { href: "/pricing", label: "Pricing" },
  { href: "/docs", label: "Docs" },
];

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
      <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.09 3.29 9.4 7.86 10.93.57.1.79-.25.79-.55 0-.27-.01-1.16-.02-2.11-3.2.7-3.88-1.36-3.88-1.36-.52-1.34-1.28-1.69-1.28-1.69-1.05-.72.08-.71.08-.71 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.18 1.18a11 11 0 0 1 5.79 0c2.2-1.49 3.17-1.18 3.17-1.18.64 1.59.24 2.76.12 3.05.74.8 1.19 1.83 1.19 3.09 0 4.43-2.7 5.41-5.26 5.69.42.36.78 1.07.78 2.16 0 1.56-.01 2.82-.01 3.2 0 .31.21.66.79.55A10.52 10.52 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z" />
    </svg>
  );
}

function Logo() {
  return (
    <Link href="/" className="flex items-center gap-2">
      <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-emerald-400 to-emerald-600 text-sm font-bold text-white">
        F
      </span>
      <span className="text-sm font-semibold tracking-tight text-slate-900">Fluxen</span>
    </Link>
  );
}

export function Nav() {
  return (
    <header className="sticky top-0 z-50 border-b border-slate-200/80 bg-white/80 backdrop-blur-md">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-6 py-3.5">
        <Logo />
        <nav className="hidden gap-6 lg:flex">
          {LINKS.map((l) => (
            <Link key={l.href} href={l.href} className="text-sm text-slate-600 transition-colors hover:text-slate-900">
              {l.label}
            </Link>
          ))}
        </nav>
        <div className="flex items-center gap-3">
          <a
            href={GITHUB_URL}
            target="_blank"
            rel="noreferrer"
            className="hidden items-center gap-1.5 rounded-lg border border-slate-200 px-3 py-1.5 text-sm font-medium text-slate-700 transition-colors hover:border-slate-300 hover:bg-slate-50 sm:flex"
          >
            <GitHubIcon className="h-4 w-4" />
            GitHub
          </a>
          <Link
            href="/docs"
            className="rounded-lg bg-slate-900 px-3.5 py-1.5 text-sm font-medium text-white shadow-sm transition-all hover:bg-slate-800 hover:shadow-md"
          >
            Get started
          </Link>
        </div>
      </div>
    </header>
  );
}

const FOOTER_COLUMNS = [
  {
    heading: "Product",
    links: [
      { href: "/product", label: "Product" },
      { href: "/how-it-works", label: "How it Works" },
      { href: "/why-fluxen", label: "Why Fluxen" },
      { href: "/providers", label: "Providers" },
    ],
  },
  {
    heading: "Resources",
    links: [
      { href: "/docs", label: "Docs" },
      { href: "/pricing", label: "Pricing" },
      { href: GITHUB_URL, label: "GitHub", external: true },
    ],
  },
];

export function Footer() {
  return (
    <footer className="border-t border-slate-200 bg-slate-50/50">
      <div className="mx-auto max-w-5xl px-6 py-14">
        <div className="grid grid-cols-1 gap-10 sm:grid-cols-[1.5fr_1fr_1fr]">
          <div>
            <Logo />
            <p className="mt-3 max-w-xs text-sm leading-relaxed text-slate-500">
              AI Traffic, Optimized. Self-hosted, open, and free of vendor lock-in.
            </p>
            <a
              href={GITHUB_URL}
              target="_blank"
              rel="noreferrer"
              className="mt-4 inline-flex items-center gap-1.5 text-sm font-medium text-slate-600 hover:text-slate-900"
            >
              <GitHubIcon className="h-4 w-4" />
              ManojVihari/Fluxen
            </a>
          </div>
          {FOOTER_COLUMNS.map((col) => (
            <div key={col.heading}>
              <p className="mb-3 text-xs font-semibold uppercase tracking-wider text-slate-400">{col.heading}</p>
              <ul className="space-y-2.5">
                {col.links.map((l) => (
                  <li key={l.href}>
                    {"external" in l && l.external ? (
                      <a href={l.href} target="_blank" rel="noreferrer" className="text-sm text-slate-600 hover:text-slate-900">
                        {l.label}
                      </a>
                    ) : (
                      <Link href={l.href} className="text-sm text-slate-600 hover:text-slate-900">
                        {l.label}
                      </Link>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
        <div className="mt-12 border-t border-slate-200 pt-6 text-xs text-slate-400">
          © {new Date().getFullYear()} Fluxen. Open source, self-hosted.
        </div>
      </div>
    </footer>
  );
}
