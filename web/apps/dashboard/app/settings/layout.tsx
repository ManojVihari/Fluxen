"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { TopNav } from "@/components/top-nav";

// Settings' shell: persistent tab nav across its four screens (Part
// I.6: "Providers, Pricing, Users, Retention").
const TABS = [
  { href: "/settings/providers", label: "Providers" },
  { href: "/settings/pricing", label: "Pricing" },
  { href: "/settings/users", label: "Users" },
  { href: "/settings/retention", label: "Retention" },
];

export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <>
      <TopNav />
      <main className="mx-auto max-w-3xl p-8">
        <div className="mb-6">
          <h1 className="text-xl font-semibold text-slate-900">Settings</h1>
        </div>

        <nav className="mb-6 flex gap-1 border-b border-slate-200">
          {TABS.map((tab) => (
            <Link
              key={tab.href}
              href={tab.href}
              className={`border-b-2 px-3 py-2 text-sm font-medium transition-colors ${
                pathname === tab.href
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-slate-500 hover:text-slate-900"
              }`}
            >
              {tab.label}
            </Link>
          ))}
        </nav>

        {children}
      </main>
    </>
  );
}
