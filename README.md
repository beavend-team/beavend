<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/logo_dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/logo_light.svg">
    <img alt="Beavend Logo" src="docs/logo_light.svg" width="280">
  </picture>
</p>

# Beavend: The Self-Hosted AI Backend for Your Apps

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8.svg?logo=go)](go.mod)
[![Architecture](https://img.shields.io/badge/Architecture-Modular_Monolith-purple.svg)](docs/architecture.md)
[![Docs](https://img.shields.io/badge/Docs-GitHub_Pages-success.svg)](website/README.md)

> **Bring your users, your data, your models.**  
> An open-source, provider-agnostic, application-facing AI backend for mobile, web, and client applications.

---

## What is Beavend?

**Beavend** provides a managed AI application runtime analogous to Firebase AI Logic while remaining **100% open-source, self-hostable, and provider-agnostic**.

It sits between client applications (iOS, Android, Kotlin Multiplatform, React) and AI model providers (Google Gemini, OpenAI, Anthropic Claude, DeepSeek, or local Ollama instances), mediating live business data access on behalf of authenticated end users.

```mermaid
flowchart LR
    Client["Client App<br/>(iOS / Android / Web)"]
    Runtime["Beavend Runtime<br/>(Data Plane)"]
    Downstream["Your APIs & DBs<br/>(HTTP / MCP / SQL)"]
    Providers["AI Providers<br/>(Gemini / OpenAI / Ollama)"]

    Client -->|User JWT| Runtime
    Runtime -->|Identity-Bound Tools| Downstream
    Runtime -->|Chat & Streaming| Providers
```

---

## Key Architectural Guarantees

* ⚡ **Zero-DB Request Hot Path**: All project configurations, CEL policies, model routes, and tool schemas compile into immutable in-memory snapshots (`atomic.Pointer`). Handlers never touch PostgreSQL on the request path.
* 🛡️ **Bring Your Existing Auth**: Direct validation of JWTs issued by Firebase Auth, Supabase Auth, or generic OIDC. Automated JWKS caching, key rotation, and clock skew tolerance without managing user tables.
* 🔒 **Confused-Deputy Defense**: Tools declare `identity_bindings` to strictly discard LLM-supplied user parameters and inject server-verified claims from the caller's JWT.
* 🌐 **Automated SSRF Guard**: Downstream HTTP tool executions use a DNS-rebinding-safe dialer that automatically blocks loopback, private RFC1918 subnets, and cloud metadata endpoints with zero configuration required.
* 🔀 **Universal Model Routing**: Abstract logical models (`fast`, `smart`, `code`) with automatic failover, weighted load distribution, and provider mediation via LiteLLM and native OpenAI-compatible adapters.
* 🛠️ **Advanced Tool Executors**: Native support for REST HTTP endpoints, Model Context Protocol (MCP) servers, and safe parameterized SQL templates.
* ⏸️ **Human-in-the-Loop Confirmation**: Side-effect write tools automatically suspend execution, issue tamper-proof signed HMAC tokens, and resume statelessly via Valkey/Redis.
* 🌊 **Streaming First**: Native HTTP/2 Server-Sent Events (SSE) streaming with immediate client cancellation propagation to abort upstream LLM calls.
* 📦 **Configuration as Code (GitOps)**: Export and import complete tenant environments via declarative YAML/JSON files with secret encryption and environment promotion (`development` → `staging` → `production`).
* 💻 **Single Binary Deployment**: Modular monolith in Go running as `--role=runtime`, `--role=control`, or `--role=all` with an embedded React admin dashboard.

---

## Quickstart (Local Development)

Start the entire local stack (PostgreSQL with `pgvector`, Valkey/Redis, SearXNG, and Beavend) with Docker Compose:

```bash
docker compose -f deploy/docker/docker-compose.yml up -d
```

Open the embedded Admin Dashboard:
* **URL**: [http://localhost:8080/](http://localhost:8080/)
* **Default Admin Email**: `admin@beavend.local`
* **Default Admin Password**: `adminpassword123`

Verify health probes:
```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

---

## Building and Running with Docker

Beavend is packaged as a lightweight, multi-stage Docker image (~45 MB) that bundles the compiled Go binary and the embedded React dashboard SPA.

### 1. Build the Docker Image Locally

Run the build from the repository root:

```bash
# Standard build
docker build -t beavend:latest -f deploy/docker/Dockerfile .

# Cross-compilation (e.g., building for AMD64 on Apple Silicon)
docker build --platform linux/amd64 -t beavend:latest -f deploy/docker/Dockerfile .
```

### 2. Multi-Stage Build Architecture
The build executes across three isolated stages:
1. **Frontend Stage (`node:20-alpine`)**: Installs dashboard dependencies and builds the production React/Vite SPA bundle (`dashboard/dist`).
2. **Compiler Stage (`golang:1.26-alpine`)**: Downloads Go modules and compiles the static Go binary, embedding `dashboard/dist` via Go's native `embed.FS`.
3. **Runtime Stage (`alpine:3.20`)**: Minimal image containing only the compiled `/app/beavend` binary, CA certificates, and timezone data.

### 3. Run Standalone Container

If you have existing PostgreSQL and Redis instances:

```bash
docker run -d \
  --name beavend \
  -p 8080:8080 \
  -e BEAVEND_ENV=production \
  -e BEAVEND_ROLE=all \
  -e BEAVEND_HTTP_PORT=8080 \
  -e BEAVEND_DATABASE_URL="postgres://beavend:beavend@db.internal:5432/beavend?sslmode=disable" \
  -e BEAVEND_REDIS_URL="redis://valkey.internal:6379/0" \
  -e BEAVEND_MASTER_KEY="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" \
  -e BEAVEND_ADMIN_EMAIL="admin@yourdomain.com" \
  -e BEAVEND_ADMIN_PASSWORD="YourSecurePassword" \
  beavend:latest
```

### 4. Operational Roles

Beavend supports operational role separation via the `--role` flag or `BEAVEND_ROLE` environment variable:
* `BEAVEND_ROLE=all` *(Default)*: Runs both runtime and control planes in a single process.
* `BEAVEND_ROLE=runtime`: Mounts only end-user AI traffic endpoints (`/v1/apps/...`) and streaming pipelines. Stateless and horizontally autoscalable.
* `BEAVEND_ROLE=control`: Mounts only management API endpoints (`/v1/admin/...`) and the embedded dashboard UI.

---

## Documentation Portal

Interactive documentation powered by Docsify is located in [`website/`](website/README.md) and automatically deployed to GitHub Pages:

| Guide | Description |
|---|---|
| [**Quickstart Guide**](website/getting-started/quickstart.md) | Initial setup, configuration, and testing your first AI request. |
| [**Docker Guide**](website/getting-started/docker.md) | Multi-stage build mechanics, container flags, and orchestration. |
| [**Architecture & Call Flows**](website/architecture/overview.md) | Deep architectural analysis and call flows derived from Graphify AST extraction. |
| [**Authentication & Identity**](website/guides/auth-and-identity.md) | Setup Firebase Auth, Supabase Auth, or OIDC, JWKS caching, and claims. |
| [**Model Routing & Providers**](website/guides/model-routing-and-providers.md) | LiteLLM, OpenAI-compatible endpoints, failovers, and weighted routing. |
| [**Beavend CEL Policy Engine**](website/guides/cel-policies.md) | Authoring in-process CEL policies for app authorization and tool defense. |
| [**Tools, MCP & SQL Templates**](website/guides/tools-and-executors.md) | Setting up REST tools, MCP servers, parameterized SQL, and identity bindings. |
| [**Configuration as Code**](website/guides/config-as-code.md) | CLI export/import, GitOps workflows, and environment promotion. |
| [**API Reference**](website/reference/api.md) | Runtime REST & SSE streaming contracts, tool confirmations, and error taxonomy. |

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

Licensed under the [Apache-2.0 License](LICENSE).
