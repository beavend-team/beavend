# AGENTS.md — Beavend Backend Contributor & AI Agent Guide

Welcome to the **Beavend** backend repository. This document provides practical, non-generic instructions for AI coding agents and human engineers contributing to the server, control plane, and administrative dashboard.

---

## 1. Project Purpose & Scope

**Beavend** is an open-source, self-hosted AI application backend designed for mobile, web, and client applications. It serves as an application-facing AI runtime—analogous to Firebase AI Logic—while remaining:
* **Self-hostable & Open-Source:** Deployed as a single binary via Docker Compose or Kubernetes.
* **Provider-Agnostic:** Mediates between client applications and AI model providers via LiteLLM and native adapters.
* **Data-Agnostic:** Stores **zero** implementor business data. Live application data is fetched at request-time via custom Tools on behalf of authenticated end users.
* **Identity-Provider Agnostic:** Consumes and validates identity tokens (JWTs) issued by the implementor's existing auth provider (Firebase Auth, Supabase Auth, generic OIDC).

**Ownership Model:**
* `docs/PRD.md` — What the product does and why.
* `docs/architecture.md` — How the system is structured.
* `docs/techstack.md` — Exact technology and library choices.
* `docs/decisions/` — Architectural Decision Records (ADRs).
* `docs/api.md` — API design contracts.
* `docs/security.md` — Security requirements and boundaries.
* `docs/development.md` — Local setup, testing, and validation commands.
* `tasks/` — Executable implementation roadmap and tasks.

---

## 2. High-Level Architecture & Planes

Beavend is architected as a **modular monolith** running a single compiled Go binary with a strict split between two operational planes:

1. **Runtime Plane (Data Plane):**
   * Serves public client AI requests (`POST /v1/apps/{id}/chat`, `POST /v1/apps/{id}/stream`).
   * High volume, low latency, stateless, horizontally scalable.
   * **Crucial Invariant:** The request hot-path **never queries PostgreSQL for configuration**. It serves requests entirely from in-memory compiled configuration snapshots.
   * Authenticates end users via validated JWTs.
2. **Control Plane (Management Plane):**
   * Serves administrative users and the web dashboard (`/v1/admin/...`).
   * Lower volume, reads and writes durable state in PostgreSQL, logs audit events.
   * Publishes config revisions to Redis pub/sub (with Postgres LISTEN/NOTIFY fallback).
   * Embeds the compiled React/Vite dashboard SPA using Go's `embed.FS`.

The binary accepts `--role=runtime`, `--role=control`, or `--role=all` (default for single-node installs).

---

## 3. Repository Structure

```text
beavend/
├── api/
│   └── openapi.yaml           # Single source of truth for runtime & management APIs
├── cmd/
│   └── beavend/            # Application entrypoint: flags, role wiring, graceful shutdown
├── internal/
│   ├── core/                  # Pure domain entities, types, and interfaces (zero outward dependencies)
│   ├── runtime/               # Request pipeline stages, agent loop, SSE stream writer
│   │   ├── stages/            # Pipeline stage implementations (Stage interface)
│   │   ├── agent/             # Bounded agent loop & tool orchestration
│   │   └── stream/            # SSE event formatting & flusher handling
│   ├── control/               # Admin HTTP handlers, RBAC, config management, audit logging
│   ├── identity/              # JWT/OIDC/Firebase/Supabase validation & JWKS caching
│   ├── policy/                # Google CEL compilation & policy evaluation
│   ├── limits/                # Redis Lua script rate limiter & token quota reconciler
│   ├── prompts/               # Versioned prompt templating engine
│   ├── routing/               # Logical model to provider deployment resolver
│   ├── tools/                 # Tool engine: HTTP executor, identity binder, SSRF guard
│   ├── guardrails/            # Input/output guardrails (size, PII, injection defense)
│   ├── memory/                # Opt-in context/memory (pgvector MemoryProvider)
│   ├── cache/                 # Redis response and tool cache
│   ├── providers/             # LiteLLM adapter & native Go OpenAI-compatible adapter
│   ├── snapshot/              # In-memory config compiler, atomic pointer swap, pub/sub sync
│   ├── usage/                 # Async bounded channel pipeline, batch writer to DB
│   ├── store/                 # Database schema migrations (goose) and queries (sqlc)
│   ├── secrets/               # Envelope encryption (AES-256-GCM) with pluggable KeyProvider
│   ├── telemetry/             # OpenTelemetry tracing, Prometheus metrics, structured slog
│   └── config/                # Environment and file configuration loading
├── dashboard/                 # React 18 + TypeScript + Vite + Tailwind admin SPA
├── deploy/
│   └── docker/                # Dockerfile and docker-compose.yml
├── docs/                      # Engineering documentation and ADRs
└── tasks/                     # Actionable implementation tasks and roadmap
```

---

## 4. Architectural Invariants & Constraints

Every agent and contributor must uphold these non-negotiable rules:

1. **Zero DB Reads on the Hot Path:**
   * Runtime instances must compile project configuration into an in-memory `Snapshot` struct upon notification and swap it atomically (`atomic.Pointer`).
   * Never execute a database query in a runtime request handler or pipeline stage to fetch AI App, prompt, policy, model, or tool settings.
2. **Strict Inward Dependency Flow:**
   * Domain interfaces live in `internal/core`. Leaf packages (e.g. `internal/providers`, `internal/tools`) depend on `internal/core`.
   * `internal/core` must **never** import leaf packages.
   * No package may import `internal/control` or `internal/runtime` except `cmd/beavend`.
   * Enforced via `depguard` in `golangci-lint`.
3. **No ORM Magic:**
   * All database interactions must use raw `.sql` files processed by `sqlc` to generate type-safe Go code.
   * Migrations must be versioned SQL managed by `goose`.
4. **Token Revocation Out-of-Scope:**
   * Beavend **validates and forwards** tokens. It does not track token revocation lists, active sessions, or user state. Token revocation belongs strictly to the implementor's identity provider.
5. **No Provider Credentials in Clients or Responses:**
   * AI provider API keys and LiteLLM keys must never be exposed to clients, returned in API payloads, or written into logs.
6. **Stateless Scalability:**
   * No in-memory session affinity. Suspended operations (human-in-the-loop tool confirmations or client-side tools) must persist state in Redis with a 5-minute TTL so any runtime replica can resume the request.

---

## 5. Coding Conventions

* **Language & Standard Library First:** Use Go standard library idioms where possible. Use `net/http` combined with `chi` router for routing.
* **Structured Logging:** Use `log/slog` exclusively. Always include `request_id`, `project_id`, and `env` attributes. Never use `fmt.Print` or untyped logging.
* **Error Handling:** Return wrapped errors (`fmt.Errorf("...: %w", err)`). Map internal errors to the Beavend error taxonomy at the HTTP ingress stage. Never return raw internal error messages or stack traces to end users.
* **Context Propagation:** Pass `context.Context` as the first parameter in all blocking/network operations. Propagate client cancellation through `context.Context` to abort upstream LLM calls and tool executions immediately.
* **Concurrency Discipline:** SSE streams run on dedicated goroutines. Ensure explicit cleanup, avoid goroutine leaks, and use bounded channels for asynchronous usage and logging pipelines.

---

## 6. Security Requirements

1. **JWT & Identity:**
   * Verify signature using cached JWKS with automated key-rotation refresh.
   * Validate `exp`, `nbf` (with 60-second clock skew tolerance), `iss`, and `aud`.
   * Explicitly reject `none` algorithm and algorithm mismatches.
2. **Confused-Deputy Defense:**
   * When tools require user identification (e.g., `user_id`), the value **must be injected by the server from the validated token claims** via `identity_bindings`.
   * Values supplied in LLM arguments for identity-bound fields must be discarded and overwritten.
3. **Tool Execution & SSRF Prevention:**
   * HTTP tool executors must use a custom `http.Transport` with a DNS-rebinding-safe dialer that denies loopback (`127.0.0.0/8`, `::1`), private ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), and link-local/cloud metadata ranges (`169.254.169.254`).
   * Tools must strictly validate input arguments against their configured JSON Schema.
4. **Untrusted Tool Results:**
   * Data returned from tool executions must be treated as untrusted input. Results must be capped in size, redacted according to configuration, and wrapped before being fed back to the LLM.
5. **Secrets at Rest:**
   * Downstream tool secrets and provider credentials stored in PostgreSQL must be encrypted using AES-256-GCM envelope encryption.

---

## 7. LLM & Provider Abstraction Rules

* All model invocations must go through the `ProviderAdapter` interface defined in `internal/core`:
  ```go
  type ProviderAdapter interface {
      Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
      Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
      Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
      Health(ctx context.Context) error
  }
  ```
* LiteLLM (`LiteLLMAdapter`) is the default provider gateway.
* High-volume OpenAI-compatible endpoints may utilize `OpenAICompatAdapter` directly.
* Never instantiate HTTP clients directly inside pipeline stages to call OpenAI, Anthropic, or Gemini APIs.

---

## 8. Tool Execution & Confirmation Rules

* **Tool Engine Execution Loop:** The agent loop is strictly bounded by `max_depth` (default: 5 iterations).
* **Write Tools & Confirmation:** Tools marked with `side_effect: write` default to `confirmation: required`.
* When confirmation is required:
  1. Freeze loop state and store in Redis under `pend:{project}:{req_id}` with a 5-minute TTL.
  2. Issue a signed HMAC confirmation token binding `{req_id, tool_id, args_hash, user_subject}`.
  3. Emit an SSE `confirmation` event and close the stream.
  4. Client resumes via `POST /v1/apps/{id}/confirm`. Re-verify token and user before execution.

---

## 9. Testing & Validation Expectations

Before submitting any code, agents and contributors must execute the following checks:

1. **Unit Testing:**
   * Run all unit tests: `go test -race -v ./internal/...`
   * Table-driven tests are mandatory for CEL policy evaluation, rate-limit mathematics, and identity extraction.
2. **Integration Testing:**
   * Integration tests use `testcontainers-go` for real PostgreSQL and Redis/Valkey instances.
   * Run integration suite: `go test -tags=integration ./...`
3. **Static Analysis & Linting:**
   * Run `golangci-lint run ./...` (must pass with 0 errors).
   * Run `govulncheck ./...` (zero known vulnerabilities permitted).
4. **Contract Verification:**
   * Ensure any API changes are reflected in `api/openapi.yaml`.

---

## 10. Architectural Governance & ADRs

* **No Undocumented Decisions:** Do not introduce architectural deviations or new dependencies without creating or updating an ADR.
* **When to Create an ADR:**
  * Introducing a new primary dependency, database, or external communication protocol.
  * Changing the authentication, authorization, or identity normalization model.
  * Modifying the streaming protocol or SSE event taxonomy.
  * Changing the configuration snapshot synchronization model.
* ADR files must be placed in `docs/decisions/` following the format:
  `ADR-NNN — Title.md` with sections: `Status`, `Context`, `Decision`, `Consequences`, `Alternatives Considered`.
