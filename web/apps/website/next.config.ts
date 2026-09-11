import type { NextConfig } from "next";

// The public website is statically exported and deployed independently of
// the authenticated dashboard (Part B.3 of the implementation
// specification) — it is not part of docker-compose.yml's four services.
const nextConfig: NextConfig = {
  reactStrictMode: true,
  output: "export",
};

export default nextConfig;
