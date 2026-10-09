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
Phase 3: Identity & Auth [Done] Phase 5: Rate Limiting [Done] Phase 6: Provider Layer [Done]
   │                              │                              │
   ▼                              │                              │
Phase 4: Policy Engine (CEL) [Done] │                              │
   │                              │                              │
   └──────────────────────────────┴──────────────────────────────┘
                                  │
                                  ▼
                   Phase 7: Runtime Pipeline & Agent Loop [Done]
                                  │
                   Phase 8: Tool Engine & Downstream Auth [Done]
                                  │
                   Phase 9: Usage Tracking & Async Logging [Done]
                                  │
                   Phase 10: Control Plane & Embedded Dashboard [Done]
                                  │
                   Phase 11: Guardrails, Safety & Structured Output [Done]
                                  │
                   Phase 12: Ephemeral Caching & Response Acceleration [Done]
                                  │
                   Phase 13: Identity Provider Config, Per-App Settings & User Limits [Done]
                                  │
                   Phase 14: Context, Threads & Memory Engine [Done]
                                  │
                   Phase 15: Configuration as Code (Export, Import & Promotion) [Done]
                                  │
                   Phase 16: Advanced Tool Executors (MCP & Parameterized SQL) [Done]
                                  │
                   Phase 17: App-Level Authentication & External Entitlement Webhooks [Done]
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

## Phase 5: Rate Limiting & User Quotas [Completed]
* **Status:** Completed
* **Goal:** Enforce atomic multi-window user inference limits and token quotas via Redis Lua scripts with tier resolution and token reconciliation.
* **Tasks:**
  * `tasks/phase-05-rate-limiting/T016-redis-lua-limits.md` - Redis Lua multi-window limit store
  * `tasks/phase-05-rate-limiting/T017-tier-resolution.md` - Tier resolution engine via CEL evaluation
  * `tasks/phase-05-rate-limiting/T018-token-reconciliation.md` - Token reservation and asynchronous reconciliation
  * `tasks/phase-05-rate-limiting/T019-postgres-fallback-limits.md` - PostgreSQL fallback limit store
  * `tasks/phase-05-rate-limiting/T020-limits-pipeline-stage.md` - Limits pipeline stage & HTTP handling
* **Major Deliverables:**
  * Redis Lua script for multi-window sliding window rate limits.
  * Tier resolution engine mapping identity claims to limit rules.
  * Token reservation (pre-request estimate) and asynchronous post-request reconciliation.
  * PostgreSQL fallback limit counter implementation for Redis-less deployments.
* **Dependencies:** Phase 3, Phase 4.
* **Expected Outcome:** Enforces inference and token caps; returns `429 rate_limited` with `retry_after` headers when exceeded.

---

## Phase 6: Provider Abstraction & Model Routing [Completed]
* **Status:** Completed
* **Goal:** Implement the `ProviderAdapter` interface, the default LiteLLM adapter, native OpenAI-compatible adapter, and logical model routing.
* **Tasks:**
  * `tasks/phase-06-provider-layer/T021-provider-interfaces-and-wire-protocol.md` - Provider interfaces & OpenAI wire protocol
  * `tasks/phase-06-provider-layer/T022-litellm-adapter.md` - LiteLLM provider adapter
  * `tasks/phase-06-provider-layer/T023-openaicompat-adapter.md` - Native OpenAI-compatible adapter
  * `tasks/phase-06-provider-layer/T024-model-resolver-and-routing.md` - Logical model routing resolver
  * `tasks/phase-06-provider-layer/T025-model-routing-pipeline-stage.md` - Model routing pipeline stage
* **Major Deliverables:**
  * `core.ProviderAdapter` implementation for LiteLLM (`LiteLLMAdapter`).
  * `OpenAICompatAdapter` native Go adapter for high-throughput direct OpenAI endpoints.
  * Streaming SSE parser and OpenAI wire protocol conversion.
  * Logical model resolver mapping aliases (`fast`, `reasoning`) to deployments and adapters.
  * Model routing runtime stage for request pipeline.
* **Dependencies:** Phase 1, Phase 2.
* **Expected Outcome:** Server communicates with LiteLLM and OpenAI-compatible providers to execute completions and streams with normalized error handling and zero-DB-read hot-path resolution.

---

## Phase 7: AI Runtime Pipeline & Agent Loop [Completed]
* **Status:** Completed
* **Goal:** Implement the sequential stage execution pipeline, prompt templating, and Server-Sent Events (SSE) streaming delivery.
* **Tasks:**
  * `tasks/phase-07-runtime-pipeline/T026-prompt-templating-engine.md` - Prompt templating engine
  * `tasks/phase-07-runtime-pipeline/T027-prompt-pipeline-stage.md` - Prompt pipeline stage
  * `tasks/phase-07-runtime-pipeline/T028-sse-stream-flusher.md` - SSE stream flusher & heartbeat engine
  * `tasks/phase-07-runtime-pipeline/T029-pipeline-orchestrator-and-execution-stage.md` - Pipeline orchestrator & execution stage
  * `tasks/phase-07-runtime-pipeline/T030-runtime-http-handlers-and-server-wiring.md` - Runtime HTTP handlers & server wiring
* **Major Deliverables:**
  * Stage pipeline orchestrator (`Stage` interface).
  * Prompt engine with variable interpolation from configuration and identity claims.
  * SSE flusher with heartbeat comments and graceful client-disconnect propagation.
  * `POST /v1/apps/{id}/chat` and `POST /v1/apps/{id}/stream` handlers.
* **Dependencies:** Phase 2, Phase 3, Phase 4, Phase 5, Phase 6.
* **Expected Outcome:** Working end-to-end unary and streaming chat completions.


---

## Phase 8: Tool Engine & Downstream Auth [Completed]
* **Status:** Completed
* **Goal:** Implement custom tool orchestration, JSON Schema validation, identity binding injection, SSRF-safe HTTP execution, and human-in-the-loop confirmation.
* **Tasks:**
  * `tasks/phase-08-tools/T031-ssrf-guard-and-safe-dialer.md` - SSRF guard & DNS-rebinding-safe dialer
  * `tasks/phase-08-tools/T032-downstream-auth-and-http-executor.md` - Downstream auth strategies & HTTP tool executor
  * `tasks/phase-08-tools/T033-tool-engine-bindings-validation-sanitizer.md` - Identity bindings, schema validation & untrusted result sanitizer
  * `tasks/phase-08-tools/T034-suspended-state-redis-and-confirmation-token.md` - Suspended state continuation & HMAC confirmation tokens
  * `tasks/phase-08-tools/T035-bounded-agent-loop-and-runtime-endpoints.md` - Bounded agent loop & runtime confirmation endpoints
* **Major Deliverables:**
  * Tool engine with bounded loop depth (max iterations).
  * Hardened HTTP executor with custom `SafeDialer` blocking private/metadata IPs.
  * Identity bindings injecting `user_id` from token claims.
  * Redis-backed suspended execution state and HMAC confirmation token issuance.
  * `POST /v1/apps/{id}/confirm` and `/tool-results` endpoints.
* **Dependencies:** Phase 7.
* **Expected Outcome:** LLM can invoke external APIs with user identity propagation and pause for write tool confirmations.

---

## Phase 9: Usage Tracking & Async Logging [Completed]
* **Status:** Completed
* **Goal:** Implement asynchronous bounded channel logging and usage tracking writing to partitioned PostgreSQL tables.
* **Tasks:**
  * `tasks/phase-09-usage-tracking/T036-usage-schema-and-partitions.md` - PostgreSQL partitioned tables for request logs & usage events
  * `tasks/phase-09-usage-tracking/T037-bounded-event-buffer-and-batch-writer.md` - Bounded event buffer & batch writer
  * `tasks/phase-09-usage-tracking/T038-usage-recording-and-token-reconciliation.md` - Runtime usage recording & token reconciliation
  * `tasks/phase-09-usage-tracking/T039-usage-rollups-and-aggregation.md` - Usage rollup background aggregator
  * `tasks/phase-09-usage-tracking/T040-server-wiring-and-end-to-end-tests.md` - Server wiring & end-to-end validation
* **Major Deliverables:**
  * Bounded in-process event channel with configurable drop/backpressure policies.
  * Batch writer persisting request metadata logs and usage events.
  * Hourly and daily usage rollup background aggregator.
* **Dependencies:** Phase 7.
* **Expected Outcome:** Non-blocking request completion with auditable usage metrics and metadata logs.

---

## Phase 10: Control Plane & Embedded Dashboard [Completed]
* **Status:** Completed
* **Goal:** Build the administrative management API, admin session authentication, audit logging, and embed the React/Vite dashboard.
* **Tasks:**
  * `tasks/phase-10-control-plane/T041-admin-auth-sessions-rbac-and-audit.md` - Admin authentication, sessions, RBAC & audit logging
  * `tasks/phase-10-control-plane/T042-management-api-crud-and-snapshot-invalidation.md` - Management API: core resources CRUD & snapshot invalidation
  * `tasks/phase-10-control-plane/T043-management-api-observability-logs-and-privacy.md` - Management API: observability, logs & privacy operations
  * `tasks/phase-10-control-plane/T044-react-vite-admin-dashboard-spa.md` - React + Vite admin dashboard single page application
  * `tasks/phase-10-control-plane/T045-embedded-dashboard-serving-server-wiring-and-e2e.md` - Embedded dashboard serving, server wiring & end-to-end validation
* **Major Deliverables:**
  * REST endpoints under `/v1/admin/...` for projects, apps, prompts, models, tools, and policies.
  * Local admin account authentication, bcrypt hashing, and session cookie / bearer token management.
  * Synchronous audit logging for mutating administrative operations.
  * Automatic project revision increment and snapshot invalidation notification.
  * React + TypeScript + Vite SPA embedded in Go binary via `embed.FS`.
* **Dependencies:** Phase 2.
* **Expected Outcome:** Administrators can configure Beavend entirely through a web browser UI or management API.

---

## Phase 11: Guardrails, Content Safety & Structured Output [Completed]
* **Status:** Completed
* **Goal:** Implement the runtime input/output guardrail engine, PII detection and redaction, prompt injection defense, structured JSON schema validation, and streaming safety modes (`buffered` vs `passthrough_with_post_check`).
* **Tasks:**
  * `tasks/phase-11-guardrails-safety/T046-guardrail-engine-and-pii.md` - Guardrail engine, PII detection & redaction (Luhn credit cards, SSNs, emails, phones, tokens)
  * `tasks/phase-11-guardrails-safety/T047-prompt-injection-defense-and-size-limits.md` - Prompt injection defense & payload size bounds
  * `tasks/phase-11-guardrails-safety/T048-structured-output-validation.md` - Structured output JSON Schema validation & snapshot pre-compilation
  * `tasks/phase-11-guardrails-safety/T049-guardrails-pipeline-stages-and-streaming.md` - Input/Output guardrail pipeline stages & streaming execution modes
  * `tasks/phase-11-guardrails-safety/T050-server-wiring-error-taxonomy-and-e2e-tests.md` - Server wiring, HTTP 422 mapping, metrics & end-to-end tests
* **Major Deliverables:**
  * Fast modular guardrail engine in `internal/guardrails` satisfying `core.Guardrail`.
  * Luhn-validated credit card, SSN, email, phone, and token detection/redaction.
  * Multi-heuristic prompt injection defense preventing jailbreaks and system prompt overrides.
  * Structured output JSON Schema validator with markdown code-fence stripping.
  * `InputGuardrailStage` and `OutputGuardrailStage` in `internal/runtime/stages`.
  * Streaming delivery modes: safe `buffered` mode and low-latency `passthrough_with_post_check` mode.
  * Standardized HTTP 422 `guardrail_blocked` error handling and Prometheus violation metrics.
* **Dependencies:** Phase 7, Phase 10.
* **Expected Outcome:** AI Apps enforce strict content safety, zero-PII leakage, injection resilience, and guaranteed structured output without database queries on the request hot path.

---

## Phase 12: Ephemeral Caching & Response Acceleration [Completed]
* **Status:** Completed
* **Goal:** Build tenant and identity-scoped Redis response and tool caching with TTL invalidation to reduce model latency and upstream token costs.
* **Tasks:**
  * `tasks/phase-12-caching/T051-cache-store-implementations-and-subject-purge.md` - Cache store implementations (`RedisCacheStore`, `MemoryCacheStore`), key builders & subject purge
  * `tasks/phase-12-caching/T052-response-caching-pipeline-stage.md` - Response caching pipeline stage (`CacheStage`) & streaming SSE replay
  * `tasks/phase-12-caching/T053-tool-result-caching-in-agent-loop.md` - Tool result caching in bounded agent loop
  * `tasks/phase-12-caching/T054-observability-headers-and-privacy-purge.md` - Observability metrics, `X-Cache` headers, post-execution storage & privacy deletion
  * `tasks/phase-12-caching/T055-server-wiring-adr-and-e2e-tests.md` - Server wiring, ADR-011 & end-to-end validation suite
* **Major Deliverables:**
  * Concurrency-safe `MemoryCacheStore` and production-grade `RedisCacheStore` implementing `core.CacheStore` and `cache.PurgeableCacheStore`.
  * Canonical key builders adhering to Architecture §7.3 (`rc:{project}:{env}:{scope}:{hash}`, `tc:{project}:{env}:{user}:{tool}:{argshash}`).
  * `CacheStage` in `internal/runtime/stages` supporting synchronous completion and SSE streaming replay.
  * Tool execution caching in `internal/runtime/agent/loop.go` with user isolation and TTL expiration.
  * Standardized `X-Cache: HIT` / `X-Cache: MISS` HTTP response headers and Prometheus `beavend_cache_requests_total` metrics.
  * Subject privacy purge integration (`DELETE /v1/admin/subjects/{issuer}/{subject}`) removing all user-scoped cache keys.
  * ADR-011 in `docs/decisions/ADR-011-ephemeral-caching-and-response-acceleration.md`.
* **Dependencies:** Phase 7, Phase 8, Phase 10.
* **Expected Outcome:** Repeat queries and idempotent tool calls return in sub-millisecond time with zero upstream token costs, while preserving user isolation and privacy compliance.

---

## Phase 13: Identity Provider Config, Per-App Settings & User Limits [Completed]
* **Status:** Completed
* **Goal:** Implement persistent Identity Provider configuration (`AuthConfig`) in PostgreSQL with zero-DB hot-path compilation, dedicated Auth management with live JWT inspection, full Per-App Settings (limits, tiers, guardrails, tools, caching, schemas), and an Interactive Playground.
* **Tasks:**
  * `tasks/phase-13-auth-and-app-settings/T056-auth-config-schema-and-snapshot.md` - AuthConfig database schema, sqlc queries & snapshot compilation
  * `tasks/phase-13-auth-and-app-settings/T057-control-plane-auth-and-app-endpoints.md` - Control plane AuthConfig and AI App management endpoints
  * `tasks/phase-13-auth-and-app-settings/T058-dashboard-app-detail-and-settings.md` - Dashboard AI App details, per-app settings & interactive playground
  * `tasks/phase-13-auth-and-app-settings/T059-dashboard-identity-provider-settings.md` - Dashboard Identity Provider configuration & JWT inspector
  * `tasks/phase-13-auth-and-app-settings/T060-verification-and-roadmap-update.md` - Integration verification & roadmap update
* **Major Deliverables:**
  * Migration `00004_auth_config.sql` adding `auth_config JSONB` to `environments` table.
  * Snapshot compiler populating `snap.AuthConfig` from environment configuration.
  * Control plane endpoints: `GET/PUT /v1/admin/projects/{project_id}/environments/{name}/auth` and `POST /v1/admin/auth/test-token`.
  * AI App Workspace with sub-tabs for Overview & SDK snippets, Per-App Settings (Prompts, Model Routing, Attached Tools, Rate Limits with CEL Tiers, Safety & Guardrails, Response Caching, Structured Output Schemas), and an Interactive Test Playground.
  * Settings page overhaul with dedicated Identity Provider configuration (Firebase, Supabase, Generic OIDC) and live JWT Token Inspector.
## Phase 14: Context, Threads & Memory Engine [Completed]
* **Status:** Completed
* **Goal:** Implement server-side conversation threads with TTL, user-scoped semantic vector memory, pluggable memory providers (pgvector, HTTP, local), and GDPR privacy subject purging.
* **Tasks:**
  * `tasks/phase-14-context-threads-memory/T061-memory-provider-and-thread-store-interfaces.md` - Core MemoryProvider & ThreadStore domain interfaces and entities
  * `tasks/phase-14-context-threads-memory/T062-pgvector-and-sqlc-storage.md` - PostgreSQL + pgvector schema migration, sqlc queries & PGVectorMemoryProvider
  * `tasks/phase-14-context-threads-memory/T063-http-and-local-memory-providers.md` - Implementor-hosted HTTP memory provider & in-memory local provider
  * `tasks/phase-14-context-threads-memory/T064-conversation-thread-stores-and-runtime-endpoints.md` - Redis, Postgres & Memory thread stores, turn appending & HTTP endpoints
  * `tasks/phase-14-context-threads-memory/T065-context-stage-pipeline-integration-and-gdpr-purge.md` - ContextStage pipeline integration, GDPR subject purge hook & ADR-012
* **Major Deliverables:**
  * Core interfaces `core.MemoryProvider` (`Store`, `Recall`, `Search`, `DeleteBySubject`) and `core.ThreadStore` (`GetThread`, `CreateThread`, `AppendMessages`, `DeleteThread`, `DeleteThreadsBySubject`).
  * Migration `00005_context_and_memory.sql` adding `conversation_threads`, `semantic_memories` with pgvector, and `memory_config` on `ai_app_versions`.
  * Production providers: `PGVectorMemoryProvider` (PostgreSQL + pgvector cosine similarity), `HTTPMemoryProvider` (SSRF-safe implementor-hosted HTTP client), and `LocalMemoryProvider`.
  * Thread stores: `RedisThreadStore` (native TTL, user sets index), `PostgresThreadStore`, and `MemoryThreadStore`.
  * `ContextStage` in `internal/runtime/stages` hydrating thread history and recalling semantic memories into request turns.
  * Runtime REST endpoints: `GET/DELETE /v1/apps/{ai_app_id}/threads/{thread_id}`, `POST /v1/apps/{ai_app_id}/threads`.
  * Turn appending in `HandleChat` and `HandleStream` saving conversation message turns with configured TTL.
  * GDPR privacy deletion hook in `DELETE /v1/admin/subjects/{issuer}/{subject}` purging memories and threads.
  * ADR-012 in `docs/decisions/ADR-012-conversation-threads-and-vector-memory-engine.md`.
* **Dependencies:** Phase 2, Phase 7, Phase 10, Phase 12.
* **Expected Outcome:** Client applications seamlessly conduct multi-turn conversations using server-side threads and receive personalized responses via user-scoped semantic memory recall with strict privacy compliance.

---

## Phase 15: Configuration as Code (Export, Import & Environment Promotion) [Completed]
* **Status:** Completed
* **Goal:** Implement declarative Configuration as Code (`beavend_version: "0.1"` YAML), secret sanitization, dry-run diffing, CLI subcommands (`beavend export` and `beavend import`), control plane export/import endpoints, and a dedicated dashboard UI for GitOps workflows and multi-environment promotion.
* **Tasks:**
  * `tasks/phase-15-config-as-code/T066-config-code-schemas-and-serializers.md` - Config-as-code schemas, YAML serialization & secret masking
  * `tasks/phase-15-config-as-code/T067-export-import-engine-and-secret-handling.md` - Export/Import engine, dry-run diff calculation & transactional apply
  * `tasks/phase-15-config-as-code/T068-control-plane-export-import-endpoints.md` - Control plane export/import HTTP endpoints & OpenAPI contract
  * `tasks/phase-15-config-as-code/T069-cli-export-import-commands.md` - CLI commands `beavend export` and `beavend import`
  * `tasks/phase-15-config-as-code/T070-dashboard-config-code-ui-and-e2e.md` - Dashboard Config-as-Code UI, ADR-013 & end-to-end verification
* **Major Deliverables:**
  * Strict declarative schema in `internal/configcode` covering Project, AuthConfig, AI Apps, Prompts, Models, Tools, Policies, and Limits.
  * Secret sanitization and environment variable interpolation (`${VAR}`, `env:NAME`).
  * Dry-run diff engine computing detailed Create/Update/Unchanged plans.
  * CLI commands `beavend export` and `beavend import` supporting `--dry-run` and direct database operation.
  * Management endpoints `GET /v1/admin/projects/{project_id}/environments/{env}/export` and `POST /v1/admin/projects/{project_id}/environments/{env}/import`.
  * Dashboard Config-as-Code panel with YAML download, editor/upload, and interactive diff preview.
  * ADR-013 in `docs/decisions/ADR-013-configuration-as-code-export-import.md`.
* **Dependencies:** Phase 2, Phase 10, Phase 13.
* **Expected Outcome:** Developers can export their entire project configuration into version-controlled YAML, review changes via dry-run diffs, promote configurations across environments, and automate deployments via CI/CD.

---

## Phase 16: Advanced Tool Executors (MCP & Parameterized SQL Templates) [Completed]
* **Status:** Completed
* **Goal:** Implement native Model Context Protocol (MCP) and parameterized read-only SQL template tool executors with connection pooling, identity bindings, and SSRF defense.
* **Tasks:**
  * `tasks/phase-16-advanced-executors/T071-mcp-tool-executor.md` - SSRF-safe MCP JSON-RPC 2.0 tool executor over HTTP/SSE
  * `tasks/phase-16-advanced-executors/T072-sql-template-tool-executor.md` - Parameterized read-only SQL template tool executor with dialect binding
  * `tasks/phase-16-advanced-executors/T073-executor-registry-and-agent-loop-integration.md` - Wire MCP and SQL executors into DefaultExecutorRegistry & agent loop
  * `tasks/phase-16-advanced-executors/T074-control-plane-and-dashboard-tool-support.md` - Control plane schema validation & dashboard UI support
  * `tasks/phase-16-advanced-executors/T075-verification-adr-and-roadmap-update.md` - ADR-014, integration tests & phase completion
* **Major Deliverables:**
  * `tools.MCPExecutor` communicating via JSON-RPC 2.0 (`tools/call`) over HTTP/SSE with downstream auth and SSRF protection.
  * `tools.SQLTemplateExecutor` with read-only statement enforcement, named parameter binding (`:param`), connection reuse, and JSON row serialization.
  * Integrated `DefaultExecutorRegistry` resolving `"http"`, `"webhook"`, `"mcp"`, and `"sql_template"`.
  * Management API validation and interactive testing endpoints for new executors.
  * Dashboard tool management UI supporting MCP and SQL template configuration.
  * ADR-014 in `docs/decisions/ADR-014-mcp-and-sql-template-tool-executors.md`.
* **Dependencies:** Phase 8, Phase 10, Phase 12.
* **Expected Outcome:** Models can invoke external MCP server tools and execute parameterized read-only database queries on behalf of users with zero SQL injection risk and zero DB reads on the hot path.

---

## Phase 17: App-Level Authentication, JWT Claims-Based CEL Policies & Scope Alignment [Completed]
* **Status:** Completed
* **Goal:** Migrate identity and authentication configuration to the AI App level (with project-level fallback inheritance), move CEL Policies and Identity & Auth menus to App scope (as dedicated tabs in AppWorkspace), remove duplicate configuration from the App Settings tab, and establish custom JWT claims as the standard authorization model for CEL policies (eliminating webhook overhead).
* **Tasks:**
  * `tasks/phase-17-app-auth-and-entitlements/T076-core-types-and-db-migrations.md` - Core domain types, DB migration & SQLC queries
  * `tasks/phase-17-app-auth-and-entitlements/T077-app-level-auth-and-snapshot-compiler.md` - App auth resolution & snapshot compiler
  * `tasks/phase-17-app-auth-and-entitlements/T078-entitlement-webhook-client-and-redis-cache.md` - JWT claims CEL evaluation model (superseding webhooks)
  * `tasks/phase-17-app-auth-and-entitlements/T079-cel-enrichment-and-authz-pipeline-stage.md` - Ingress JWT claims extraction & AuthZ stage integration
  * `tasks/phase-17-app-auth-and-entitlements/T080-control-plane-api-dashboard-and-e2e.md` - OpenAPI contract, control plane handlers, AppWorkspace tabs & E2E tests
* **Major Deliverables:**
  * Per-app `AuthConfig` with Project fallback for heterogeneous client applications.
  * In-memory snapshot compilation maintaining zero PostgreSQL queries on the hot path.
  * App-scoped "CEL Policies" tab with live sandbox, claims simulation, and quick templates.
  * App-scoped "Identity & Auth" tab with provider configuration, JWT token inspector, and "Copy Claims to Sandbox" flow.
  * Streamlined App "Settings" tab with zero duplicate policy or auth inputs.
  * ADR-016 in `docs/decisions/ADR-016-app-level-auth-and-entitlement-webhooks.md`.
* **Dependencies:** Phase 3, Phase 4, Phase 10, Phase 13.
* **Expected Outcome:** Granular, per-application authentication and CEL access policies evaluated entirely from custom JWT claims in memory with zero external webhook hops and zero DB reads on the hot path.

## Phase 18: Multimodal Model Routing [In Progress]
* **Status:** In Progress
* **Goal:** User-defined routing of inputs/outputs (image, audio, video) to arbitrary logical models with fallbacks, without breaking cache or sessions (ADR-017).
* **Tasks:**
  * `tasks/phase-18-multimodal-routing/T081-core-types-routes-and-migration.md` [Completed] - Core types, migration
  * `tasks/phase-18-multimodal-routing/T082-snapshot-compiler-validation.md` [Completed] - Rule compilation & validation
  * `tasks/phase-18-multimodal-routing/T083-route-select-stage-and-resolver.md` - Rule-based resolver, RouteSelect stage
  * `tasks/phase-18-multimodal-routing/T084-cache-key-and-attachment-digests.md` - Cache key correctness
  * `tasks/phase-18-multimodal-routing/T085-canonical-parts-and-thread-compat.md` - Canonical parts, model-agnostic threads
  * `tasks/phase-18-multimodal-routing/T086-generator-interface-and-media-tools.md` [Completed] - Generator interface, media tools
  * `tasks/phase-18-multimodal-routing/T087-control-plane-dashboard-openapi-e2e.md` - API, dashboard, E2E
* **Dependencies:** Phase 6, 7, 8, 12, 14.
* **Expected Outcome:** Implementors declare which input/output goes to which model; zero DB reads on the hot path.
