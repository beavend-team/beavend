# Beavend Implementation Roadmap

This roadmap translates the Beavend PRD, Architecture, and Tech Stack into actionable, phased development milestones.

---

## Phase Overview

```text
Phase 1: Repository Foundation & Core Types [Done]
   │
   ▼
Phase 2: Persistent Storage & Config Snapshots [Done]
   │
   ├──────────────────────────────┬──────────────────────────────┐
   ▼                              ▼                              ▼
Phase 3: Identity & Auth [Done] Phase 5: Rate Limiting      Phase 6: Provider Layer
   │                              │                              │
   ▼                              │                              │
Phase 4: Policy Engine (CEL) [Done] │
   │                              │                              │
   └──────────────────────────────┴──────────────────────────────┘
                                  │
                                  ▼
                   Phase 7: Runtime Pipeline & Agent Loop
                                  │
                   Phase 8: Tool Engine & Downstream Auth
                                  │
                   Phase 9: Usage Tracking & Async Logging
                                  │
                   Phase 10: Control Plane & Embedded Dashboard
```

---

## Phase 1: Repository Foundation & Core Types [Completed]
* **Status:** Completed
* **Goal:** Establish the Go project structure, core domain types/interfaces, application configuration, structured logging, OpenTelemetry tracing, OpenAPI 3.1 specification, and HTTP server wiring with health probes.
* **Major Deliverables:**
  * Go module initialization (`go.mod`) with techstack-approved dependencies.
  * Core domain interfaces and models in `internal/core`.
  * Configuration loading (`internal/config`) supporting environment variables and YAML.
  * Structured logging with `slog` and OpenTelemetry tracer provider setup.
  * Server entrypoint `cmd/beavend` supporting `--role` flags and graceful shutdown.
  * Unauthenticated `/healthz` and `/readyz` probes.
  * OpenAPI 3.1 contract at `api/openapi.yaml`.
* **Dependencies:** None.
* **Expected Outcome:** A compilable Go binary that runs in all three roles and responds to health checks.

---

## Phase 2: Persistent Storage & Configuration Snapshots [Completed]
* **Status:** Completed
* **Goal:** Implement the PostgreSQL schema, sqlc query generation, database migrations, configuration snapshot compiler, and Redis pub/sub revision synchronization.
* **Major Deliverables:**
  * Goose migrations for tenant tables: `projects`, `environments`, `ai_apps`, `prompts`, `models`, `tools`, `policies`.
  * Sqlc configuration and type-safe query generation in `internal/store`.
  * In-memory `Snapshot` struct and compilation logic in `internal/snapshot`.
  * Atomic swap using `atomic.Pointer[*Snapshot]`.
  * Redis pub/sub revision invalidation listener with PostgreSQL `LISTEN`/`NOTIFY` fallback.
* **Dependencies:** Phase 1.
* **Expected Outcome:** Runtime instances boot, load project configuration into memory, and update snapshots atomically upon receiving revision notifications without querying PostgreSQL on the request path.

---

## Phase 3: Identity & Authentication [Completed]
* **Status:** Completed
* **Goal:** Build the runtime identity validation pipeline supporting generic OIDC, Firebase Auth, and Supabase Auth JWTs.
* **Major Deliverables:**
  * `identity.Verifier` using `lestrrat-go/jwx` with in-memory JWKS caching and background rotation.
  * Algorithm enforcement (rejecting `none` and symmetric confusion).
  * Expiration (`exp`) and not-before (`nbf`) checking with clock skew tolerance.
  * Normalization into immutable `core.Identity`.
  * HTTP authentication middleware extracting bearer tokens and injecting `Identity` into `context.Context`.
* **Dependencies:** Phase 1, Phase 2.
* **Expected Outcome:** Ingress HTTP requests are authenticated and normalized into caller identity; invalid or expired tokens are rejected with `401 unauthenticated`.

---

## Phase 4: Authorization & Policy Engine [Completed]
* **Status:** Completed
* **Goal:** Implement the Google CEL (Common Expression Language) policy evaluation engine to authorize AI App and Tool access based on identity claims.
* **Major Deliverables:**
  * CEL program compiler integrated into snapshot compilation (`internal/policy`).
  * CEL evaluation against `core.Identity` and request metadata.
  * Authorization pipeline stage returning `403 forbidden` on denial.
* **Dependencies:** Phase 2, Phase 3.
* **Expected Outcome:** Fine-grained claim-based policies evaluated in microseconds per request.

---

## Phase 5: Rate Limiting & User Quotas [Next]
* **Status:** Ready / Next
* **Goal:** Enforce atomic multi-window user inference limits and token quotas via Redis Lua scripts with tier resolution and token reconciliation.
* **Major Deliverables:**
  * Redis Lua script for multi-window sliding window rate limits.
  * Tier resolution engine mapping identity claims to limit rules.
  * Token reservation (pre-request estimate) and asynchronous post-request reconciliation.
  * PostgreSQL fallback limit counter implementation for Redis-less deployments.
* **Dependencies:** Phase 3, Phase 4.
* **Expected Outcome:** Enforces inference and token caps; returns `429 rate_limited` with `retry_after` headers when exceeded.

---

## Phase 6: Provider Abstraction & Model Routing
* **Goal:** Implement the `ProviderAdapter` interface, the default LiteLLM adapter, and logical model routing.
* **Major Deliverables:**
  * `core.ProviderAdapter` interface definition.
  * `LiteLLMAdapter` HTTP client supporting chat completion, streaming, and error normalization.
  * Logical model resolver mapping aliases (`fast`, `reasoning`) to deployments.
* **Dependencies:** Phase 1, Phase 2.
* **Expected Outcome:** Server communicates with LiteLLM to execute completions and streams with normalized error handling.

---

## Phase 7: AI Runtime Pipeline & Agent Loop
* **Goal:** Implement the sequential stage execution pipeline, prompt templating, and Server-Sent Events (SSE) streaming delivery.
* **Major Deliverables:**
  * Stage pipeline orchestrator (`Stage` interface).
  * Prompt engine with variable interpolation from configuration and identity claims.
  * SSE flusher with heartbeat comments and graceful client-disconnect propagation.
  * `POST /v1/apps/{id}/chat` and `POST /v1/apps/{id}/stream` handlers.
* **Dependencies:** Phase 2, Phase 3, Phase 4, Phase 5, Phase 6.
* **Expected Outcome:** Working end-to-end unary and streaming chat completions.

---

## Phase 8: Tool Engine & Downstream Auth
* **Goal:** Implement custom tool orchestration, JSON Schema validation, identity binding injection, SSRF-safe HTTP execution, and human-in-the-loop confirmation.
* **Major Deliverables:**
  * Tool engine with bounded loop depth (max iterations).
  * Hardened HTTP executor with custom `SafeDialer` blocking private/metadata IPs.
  * Identity bindings injecting `user_id` from token claims.
  * Redis-backed suspended execution state and HMAC confirmation token issuance.
  * `POST /v1/apps/{id}/confirm` and `/tool-results` endpoints.
* **Dependencies:** Phase 7.
* **Expected Outcome:** LLM can invoke external APIs with user identity propagation and pause for write tool confirmations.

---

## Phase 9: Usage Tracking & Async Logging
* **Goal:** Implement asynchronous bounded channel logging and usage tracking writing to partitioned PostgreSQL tables.
* **Major Deliverables:**
  * Bounded in-process event channel with configurable drop/backpressure policies.
  * Batch writer persisting request metadata logs and usage events.
  * Hourly and daily usage rollup background aggregator.
* **Dependencies:** Phase 7.
* **Expected Outcome:** Non-blocking request completion with auditable usage metrics and metadata logs.

---

## Phase 10: Control Plane & Embedded Dashboard
* **Goal:** Build the administrative management API, admin session authentication, audit logging, and embed the React/Vite dashboard.
* **Major Deliverables:**
  * REST endpoints under `/v1/admin/...` for projects, apps, prompts, models, tools, and policies.
  * Local admin account authentication and session cookie management.
  * React + TypeScript + Vite SPA embedded in Go binary via `embed.FS`.
* **Dependencies:** Phase 2.
* **Expected Outcome:** Administrators can configure Beavend entirely through a web browser UI.
