import { defineConfig } from "@playwright/test";

// Real-browser E2E against a running docker-compose stack (dashboard on
// :3000, control API + gateway on :8080). This deliberately does NOT
// start its own dev servers — it exercises the same containers a real
// deployment runs, the gap Fluxen's own implementation notes flagged:
// every prior verification pass was curl/tsc/build-only, never a real
// browser driving the actual UI.
export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  retries: 0,
  reporter: [["list"]],
  use: {
    baseURL: process.env.DASHBOARD_URL ?? "http://localhost:3000",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
});
