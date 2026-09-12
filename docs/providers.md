# Provider setup

Fluxen supports exactly three providers: OpenAI, Google Gemini, and Ollama — behind one OpenAI-compatible gateway endpoint. Your application code always speaks the OpenAI dialect to Fluxen; Fluxen translates to whichever provider actually serves the model.

There are two ways to configure a provider's credential:

1. **Environment variables** — the simplest option, set once at deploy time.
2. **Settings → Providers** — encrypted, dashboard-managed credentials with health checks. Requires `FLUXEN_ENCRYPTION_KEY` to be set (see [Self-hosting](./self-hosting.md)). When both a dashboard-managed and an env-var credential exist for the same provider, the dashboard-managed one takes priority.

## OpenAI

Env var: `OPENAI_API_KEY`. Optionally `FLUXEN_OPENAI_BASE_URL` to point at a proxy or mock server.

Or, from Settings → Providers, click **Configure** under OpenAI and paste an API key.

Model discovery (`GET /api/v1/providers/openai/models`) passes through OpenAI's own live `/v1/models` list.

## Google Gemini

Env var: `GEMINI_API_KEY`.

Or, from Settings → Providers, click **Configure** under Google Gemini and paste an API key (from [Google AI Studio](https://aistudio.google.com/) or a Google Cloud project with the Generative Language API enabled).

Model discovery returns Fluxen's own static catalog (`gemini-1.5-pro`, `gemini-1.5-flash`, `gemini-1.5-flash-8b`, `gemini-2.0-flash`) rather than a live API call — Gemini's model list is stable enough that Fluxen doesn't need to query it on every request.

Gemini requests are translated in both directions: your OpenAI-shaped request (messages, tools, response_format, generation parameters) becomes a Gemini `generateContent` call, and Gemini's response (including function calls and safety-filter blocks) becomes an OpenAI-shaped response. A prompt Gemini refuses to generate against at all surfaces as a rejected request with a clear error, the same way OpenAI's own moderation flags do — never a silently empty response.

## Ollama

Env var: `OLLAMA_BASE_URL`, e.g. `http://localhost:11434` (local) or a Docker Compose service name (containerized).

Or, from Settings → Providers, click **Configure** under Ollama and enter the base URL. A stock Ollama install has no authentication — no API key is required.

Model discovery (`GET /api/v1/providers/ollama/models`) calls Ollama's own live `/api/tags` endpoint, so it always reflects whatever models are actually pulled on that host.

**Cost handling (frozen, by design):** Ollama has no provider invoice, and Fluxen builds no GPU/electricity cost model. Every Ollama request gets `cost_status: local` and `cost_micro: 0`. The dashboard never renders this as a bare "$0.00" — it always reads "Local · not billed," and local spend is excluded from every cost/savings total unless a view explicitly opts into showing it as its own labeled row.

**Routing:** a model name Fluxen doesn't recognize from its pricing catalog is assumed to be a locally self-hosted one and routed to Ollama, provided a credential (env var or dashboard-managed) actually exists for it — never based merely on Ollama being compiled into the binary. If no Ollama credential is configured at all, an unrecognized model name is *not* silently routed anywhere; it's treated as an unknown OpenAI/Gemini model instead.

**Verification status:** the Ollama client's translation logic (request/response, streaming NDJSON, tool calls) is covered by unit tests against hand-built fixtures. It has not been exercised against a real, running Ollama server as part of building this feature — only Gemini and OpenAI were verified end-to-end against their real APIs. If you hit an issue connecting to a real Ollama instance, please report it; the code path is real, but genuinely less battle-tested than the other two providers.

## Health checks

From Settings → Providers, click **Health check** next to any configured credential. This calls the provider's own lightweight reachability check (OpenAI/Gemini: list models; Ollama: `/api/tags`) and records the result with a timestamp, without spending on a real chat completion.
