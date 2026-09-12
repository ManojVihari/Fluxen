# Fluxen E2E

Real-browser (Playwright/Chromium) tests against a running `docker compose up` stack — not a mocked or unit-tested UI, the actual dashboard a user would open.

## Run

```bash
docker compose up -d           # from the repo root
cd web/apps/e2e
pnpm exec playwright install chromium   # once
pnpm test
```

Defaults to `http://localhost:3000` (`DASHBOARD_URL` to override) and the `owner@example.com` account created by this environment's setup wizard (`E2E_EMAIL`/`E2E_PASSWORD` to override for a different deployment).

## What's covered

- Login (valid and invalid credentials)
- Create application → connect screen → generate a real API key
- Onboarding wizard renders its three steps
- Settings → Providers lists all three providers

This is deliberately small — it exists to catch "the API works but the page doesn't render/wire up correctly" class of bug that `tsc`/`next build`/curl-based verification structurally cannot catch, not to be a full UI regression suite.
