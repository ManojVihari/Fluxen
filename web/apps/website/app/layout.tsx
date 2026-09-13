import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import { Nav, Footer } from "@/components/nav";

const inter = Inter({ subsets: ["latin"], variable: "--font-inter" });

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
    <html lang="en" className={inter.variable}>
      <body className="min-h-screen bg-white font-sans text-slate-900 antialiased">
        <Nav />
        {children}
        <Footer />
      </body>
    </html>
  );
}
