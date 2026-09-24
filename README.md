# Beavend — The Self-Hosted AI Backend for Your Apps

> **Bring your users, your data, your models.**  
> An open-source, provider-agnostic, application-facing AI backend.

---

## What is Beavend?

**Beavend** provides a managed AI application runtime for mobile, web, and client applications—delivering an experience similar to Firebase AI Logic while remaining 100% open-source, self-hostable, and independent of any single model provider or identity vendor.

Beavend sits between client applications and AI model providers, mediating live data access to your own APIs:

```text
Android / iOS / KMP / Web
            │
            │ User Identity Token (JWT)
            ▼
     Beavend Runtime
            │
            ├── Token Validation & Identity Normalization
            ├── Authorization Policies (CEL)
            ├── User Limits & Quotas (Redis Lua)
            ├── Server-Side Prompts & Model Routing
            ├── Tools & Live Data Access ──► Your APIs / DBs / MCP
            │
            ▼
     Provider Adapter (LiteLLM / Native)
            │
            ▼
AI Providers (Gemini, OpenAI, Anthropic, Ollama, etc.)
```

---

## Key Features

* **Zero-DB Request Hot Path:** All project configurations, CEL policies, and tool schemas are compiled into in-memory snapshots with zero database queries on the request path.
* **Bring Your Existing Auth:** Validates JWTs directly from Firebase Auth, Supabase Auth, or generic OIDC without managing user accounts or token revocation.
* **Secure Tool Execution:** Downstream tools propagate authenticated user identity (`forward_user_token` or server-injected `identity_bindings`) with strict SSRF defenses.
* **Human-in-the-Loop Confirmation:** Write tools suspend execution and issue tamper-proof confirmation tokens, resuming statelessly via Redis.
* **Streaming First:** Native HTTP/2 Server-Sent Events (SSE) streaming with immediate client-cancellation propagation.
* **Single Binary Deployment:** Modular monolith in Go running as `--role=runtime`, `--role=control`, or `--role=all` with embedded React admin dashboard.

---

## Quick Start (Local Development)

Start the local stack with Docker Compose:

```bash
docker compose -f deploy/docker/docker-compose.yml up -d
```

Run the server in all-in-one mode:

```bash
go run ./cmd/beavend --role=all
```

* **Runtime API:** `http://localhost:8080/v1`
* **Admin Dashboard:** `http://localhost:8080/`
* **Health Check:** `http://localhost:8080/healthz`

---

## Engineering Documentation

| Document | Purpose |
|---|---|
| [**`docs/PRD.md`**](docs/PRD.md) | Product Requirements Document: What and why |
| [**`docs/architecture.md`**](docs/architecture.md) | Modular monolith architecture, planes, and pipeline |
| [**`docs/techstack.md`**](docs/techstack.md) | Go, Postgres, Valkey/Redis, LiteLLM, React specifications |
| [**`AGENTS.md`**](AGENTS.md) | Contributor and AI coding agent guidelines |
| [**`docs/api.md`**](docs/api.md) | Human-readable REST & SSE API contracts |
| [**`docs/security.md`**](docs/security.md) | Security boundaries, SSRF prevention, and envelope encryption |
| [**`docs/development.md`**](docs/development.md) | Local development, testing, and contribution instructions |
| [**`docs/decisions/`**](docs/decisions/) | Architectural Decision Records (ADRs) |
| [**`tasks/roadmap.md`**](tasks/roadmap.md) | Multi-phase engineering roadmap |
| [**`tasks/`**](tasks/) | Granular implementation tasks with dependencies and verification |

---

## License

Licensed under the [Apache-2.0 License](https://www.apache.org/licenses/LICENSE-2.0).
