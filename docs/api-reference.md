# API reference

The control API the dashboard uses, at `/api/v1/*`. All authenticated routes require a session cookie (set by `/auth/login` or `/setup`) and are scoped to the caller's organization. This is a hand-maintained reference, not generated from an OpenAPI spec — a generated reference is on the roadmap.

For the gateway itself (`/v1/chat/completions`, what your applications call), see [Quickstart](./quickstart.md).

## Setup & auth

| Method | Path | Notes |
|---|---|---|
| `GET` | `/setup` | `{ complete: bool }` — whether an organization already exists. |
| `POST` | `/setup` | Create the organization + owner account. Only callable once. |
| `POST` | `/auth/login` | `{ email, password }` → sets a session cookie. |
| `POST` | `/auth/logout` | Clears the session. |
| `GET` | `/auth/session` | The current user/org, or 401. |

## Applications

| Method | Path | Notes |
|---|---|---|
| `POST` | `/applications` | Create an application. |
| `GET` | `/applications` | List the org's applications. |
| `GET` | `/applications/{id}/summary` | `?range=24h\|7d\|30d\|90d` — spend/requests/tokens/errors. |
| `GET` | `/applications/{id}/timeseries` | Daily points for the same range. |
| `GET` | `/applications/{id}/models` | Model/provider breakdown, sorted by cost. |
| `GET` | `/applications/{id}/score` | Latest Efficiency Score snapshot. |
| `GET` | `/applications/{id}/score/history` | `?range=` — score snapshots over time. |
| `POST` | `/applications/{id}/keys` | Issue an API key (raw key shown once). |
| `DELETE` | `/keys/{id}` | Revoke a key. |

## Opportunities, simulations, policy, measurements

| Method | Path | Notes |
|---|---|---|
| `GET` | `/opportunities` | `?status=&app_id=` — list. |
| `GET` | `/opportunities/{id}` | Full detail, including `evidence`/`recommendation`. |
| `POST` | `/opportunities/{id}/review` | Marks `open → reviewed`. |
| `POST` | `/opportunities/{id}/apply` | `{ confirm: true, simulation_id, routing }` — requires a prior simulation. |
| `POST` | `/simulations` | `{ app_id, scenario: { type: "model_mix" \| "exact_caching", ... } }`. |
| `GET` | `/simulations/{id}` | Full result, including per-model breakdown. |
| `GET` | `/applications/{id}/simulations` | List an app's simulations. |
| `GET`/`PUT` | `/applications/{id}/policy` | The current policy document; `PUT` requires `expected_version`. |
| `GET` | `/applications/{id}/policy/history` | Every mutation, with diff. |
| `POST` | `/applications/{id}/policy/revert` | `{ version, note? }`. |
| `GET` | `/measurements` | `?app_id=` — list. |
| `GET` | `/measurements/{id}` | Full detail. |
| `POST` | `/measurements/{id}/revert` | `{ confirm: true, note? }` — only valid once `regressed`. |
| `GET` | `/opportunities/{id}/measurement` | The measurement tied to one opportunity, if any. |

## Overview & Requests

| Method | Path | Notes |
|---|---|---|
| `GET` | `/overview` | `?range=` — org-wide header numbers, provider mix, top applications, opportunity feed. |
| `GET` | `/overview/timeseries` | `?range=` — org-wide daily cost/requests. |
| `GET` | `/requests` | `?app_id=&model=&status=&cache=&from=&to=&cursor=` — cursor-paginated investigation list. |
| `GET` | `/requests/{id}` | Full detail, including routing decision and policy version at request time. |

## Settings

| Method | Path | Notes |
|---|---|---|
| `GET` | `/providers/credentials` | List credentials (never includes key material). |
| `POST` | `/providers/credentials` | `{ provider, api_key?, base_url? }` — creates/replaces the active credential for that provider. |
| `POST` | `/providers/credentials/{id}/revoke` | Revoke a credential. |
| `POST` | `/providers/{credentialId}/health-check` | Ping the provider with the resolved credential. |
| `GET` | `/providers/{provider}/models` | List available models for a provider. |
| `GET` | `/pricing/catalog` | The embedded pricing catalog, read-only. |
| `GET`/`PATCH` | `/settings` | The three retention knobs (`requests_retention_days`, `body_retention_days`, `body_capture_enabled`). |
| `GET` | `/users` | List the org's users. |
| `POST` | `/users` | `{ email, role }` — creates a user with a generated one-time password (no email infrastructure in V1; share the password out of band). |

## Errors

Every error response is `{ "error": "message" }` with an appropriate HTTP status. The gateway (`/v1/*`) additionally sets an `X-Fluxen-Error-Code` header so an operator can tell a Fluxen-originated rejection from a relayed provider error without parsing the body.
