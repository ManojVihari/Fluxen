import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Fluxen — AI Traffic, Optimized.",
  description:
    "Fluxen is a self-hosted AI traffic gateway and optimization platform.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-white text-slate-900 antialiased">
        {children}
      </body>
    </html>
  );
}
