# Quickstart

Reach a working, measured optimization outcome in about 15 minutes, using only Docker Compose.

## 1. Prerequisites

Docker and Docker Compose. Nothing else — no API key, no `.env` file, no encryption key to generate. Every provider credential (OpenAI, Gemini, Ollama) is added from the dashboard after first login; see [Provider setup](./providers.md).

## 2. Clone and run

```bash
git clone https://github.com/ManojVihari/Fluxen.git
cd Fluxen
docker compose up --build
```

This starts exactly four containers: Postgres, Redis, the combined `fluxen` gateway + control binary, and the `dashboard`. Migrations apply automatically on boot.

- Dashboard: http://localhost:3000
- Gateway/control API: http://localhost:8080

## 3. First-run setup

Open the dashboard. Since no organization exists yet, you'll land on the setup wizard: create your organization name, owner email, and password. This also logs you in, and drops you into a short onboarding flow for the next two steps.

## 4. Add a provider credential

From **Settings → Providers**, add an OpenAI, Gemini, or self-hosted Ollama credential. It's encrypted at rest with a key Fluxen generates and persists itself on first boot — nothing to configure. Without a credential, the gateway still runs, but every chat request returns `503 no_provider_credential` until one exists.

## 5. Create an application and connect a client

1. From Applications, click **New application** and give it a name.
2. On the application's **Connect** tab, click **Generate API key** — the raw key is shown exactly once, with ready-to-use Python, Node, and curl snippets.
3. Point your existing OpenAI client at Fluxen by changing only its `base_url` and `api_key`:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="fx_live_...",
)

client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "hello"}],
)
```

Fluxen proxies this to the real OpenAI, prices it, and records it — your application code doesn't change.

## 6. Watch traffic show up

Within seconds, the application's **Usage & Cost** tab shows spend, requests, and tokens. Once the application clears the minimum traffic/spend floor for a detector to trust (roughly 1,000 requests and $5 of spend over 14 days — see [Concepts](./concepts.md)), the **Opportunities** tab starts surfacing evidence-backed findings.

If you don't want to wait for real traffic, `fluxenctl seed --demo` seeds a demo application with ~30 days of realistic synthetic traffic (deliberately shaped so the Model Cost detector finds something) and runs the detector immediately.

## 7. Simulate, apply, measure

From an opportunity's detail page: **Simulate** replays the recommendation against real historical requests and shows the actual cost delta. **Apply** turns a proven simulation into a real, versioned policy change — one confirmed action, enforced on the gateway immediately. **Measure** appears once applied, and gives an honest before/after verdict 7 and 14 days later. A regressed verdict is always one click from **Revert**.

## Next

- [Self-hosting guide](./self-hosting.md) for production deployment details.
- [Concepts](./concepts.md) for how the Efficiency Score, opportunities, and value labeling work.
- [Provider setup](./providers.md) to add Gemini or Ollama.
