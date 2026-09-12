"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { api } from "@/lib/api";
import { Button } from "@/components/ui";

// A small persistent top nav across the authenticated product (Overview,
// Applications, Optimizations, Requests) — Phase 7 adds enough top-level
// screens that per-page header links (Phase 1-6's pattern) stop scaling.
// Application Detail keeps its own tab nav underneath this (Part I.1) —
// this bar is only the cross-section switcher.
const LINKS = [
  { href: "/", label: "Overview" },
  { href: "/applications", label: "Applications" },
  { href: "/optimizations", label: "Optimizations" },
  { href: "/policies", label: "Policies" },
  { href: "/requests", label: "Requests" },
  { href: "/settings", label: "Settings" },
];

export function TopNav() {
  const pathname = usePathname();
  const router = useRouter();

  async function handleLogout() {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }

  return (
    <header className="border-b border-slate-200 bg-white">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-8 py-3">
        <div className="flex items-center gap-6">
          <span className="text-sm font-semibold text-slate-900">Fluxen</span>
          <nav className="flex gap-1">
            {LINKS.map((link) => {
              const active = link.href === "/" ? pathname === "/" : pathname.startsWith(link.href);
              return (
                <Link
                  key={link.href}
                  href={link.href}
                  className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                    active ? "bg-slate-100 text-slate-900" : "text-slate-500 hover:text-slate-900"
                  }`}
                >
                  {link.label}
                </Link>
              );
            })}
          </nav>
        </div>
        <Button variant="secondary" onClick={handleLogout}>
          Log out
        </Button>
      </div>
    </header>
  );
}
