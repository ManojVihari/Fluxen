import type { NextConfig } from "next";

// Phase 0: a bare Next.js config. Later phases add rewrites/env plumbing
// to talk to the fluxen control API as those endpoints come online.
const nextConfig: NextConfig = {
  reactStrictMode: true,
};

export default nextConfig;
