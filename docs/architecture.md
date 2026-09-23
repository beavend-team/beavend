# Cortexbase — Architecture

> Companion to `Cortexbase-PRD.md` and `techstack.md`.
> Goals: **easy to maintain**, **stateless and horizontally scalable to millions of users**, **secure by construction**, and **simple to self-host**.

---

## 1. Architectural Decision

**A modular monolith with a strict data-plane / control-plane split, deployed as one binary that can run in different roles.**

| Considered | Verdict |
|---|---|
| Microservices | Rejected. Operational burden (service discovery, distributed tracing across ~10 services, version skew) is too high for self-hosters and a small maintainer team. Cortexbase has no domain that needs independent scaling beyond runtime vs. control. |
| Single undifferentiated monolith | Rejected. Admin traffic (dashboard, config edits) and runtime traffic (AI requests) have very different load, availability, and security needs. |
| **Modular monolith, two roles** | **Chosen.** One codebase, one binary, one image, clean internal module boundaries. `--role=runtime`, `--role=control`, or `--role=all` (default for small installs). Scale roles independently when needed. |

Why this is the easiest to maintain: one repo, one build, one deploy artifact, in-process calls instead of network hops, and boundaries enforced by Go package rules so a module can later be extracted **only if** a real need appears.

---

## 2. System Context

```text
  Android / iOS / KMP / Web apps                Admin (browser)
             │  Bearer <user JWT>                     │  admin session (local/OIDC)
             ▼                                        ▼
   ┌──────────────────────┐                ┌──────────────────────┐
   │   RUNTIME PLANE      │                │   CONTROL PLANE      │
   │   (data plane)       │◄── config ─────│   (management API +  │
   │   stateless, N pods  │   snapshots    │    dashboard)        │
   └───┬──────┬───────┬───┘                └──────────┬───────────┘
       │      │       │                               │
       │      │       └─ live data ─► Implementor APIs / DBs / MCP servers
       │      │                                       │
       │      ▼                                       ▼
       │   Redis/Valkey  (counters, cache, pending state, pub/sub)
       │                                              │
       ▼                                              ▼
  ProviderAdapter                              PostgreSQL (config, usage, logs, memory)
   ├── LiteLLM ──► Gemini / OpenAI / Anthropic / Ollama / …
   └── OpenAI-compatible adapter ──► OpenAI-compatible endpoints
```

* **Runtime plane** serves end-user AI requests. It is on the critical path for the implementor's product and must be fast, stateless, and highly available.
* **Control plane** serves the dashboard and management API. Lower traffic, stricter admin authentication, can be down without breaking running AI apps.
* The runtime plane **never reads configuration from Postgres on the request path**. It uses in-memory config snapshots (§4).

---

## 3. Planes and Roles

| Aspect | Runtime plane | Control plane |
|---|---|---|
| Traffic | End users (high volume, streaming) | Admins (low volume) |
| Auth | End-user JWT | Admin session / SSO, RBAC |
| State | Stateless; reads snapshot; writes usage/logs async | Reads/writes Postgres config; audit log |
| Scaling | Horizontal, autoscale on concurrent streams/CPU | 1–2 replicas usually enough |
| Failure impact | Product AI features down | Cannot edit config; runtime keeps working |
| Exposed | Public (behind LB/WAF) | Private or SSO-protected |

Shared code lives in internal packages; the two planes differ only in which HTTP routers and background workers they mount. Small installs run `--role=all` in one process.

---

## 4. Config Snapshot Architecture (key to scale)

The single most important scalability decision: **the hot path touches no database for configuration.**

```text
Admin edits config (dashboard)
        │
        ▼
Control plane: validate → write Postgres (new immutable version) → bump project revision
        │
        ├── NOTIFY via Redis pub/sub (Postgres LISTEN/NOTIFY fallback)
        ▼
Every runtime instance: receives "project X revision N"
        │
        ▼
Fetch & compile snapshot:  policies → compiled CEL programs
                           prompts  → parsed templates
                           tools    → validated schemas + executors
                           models/routing/limits → resolved structures
        │
        ▼
Atomic swap into in-memory  map[project/env] → *Snapshot
```

Properties:

* **Immutable, versioned snapshots.** A request pins the snapshot it started with, so a config change mid-request never produces inconsistent behavior. The log records the exact versions used.
* **Compiled ahead of time.** CEL programs, JSON schemas, and templates are compiled once per revision, not per request.
* **Convergence and safety.** Instances also poll revision numbers periodically (every few seconds) in case a pub/sub message is missed. If Postgres is temporarily unavailable, runtime instances continue serving from their last good snapshot.
* **Cold start.** An instance loads snapshots for all projects (or lazily per project) on boot and reports ready only after the initial load.

Result: per-request overhead is CPU-only work on local memory, which supports the PRD budget of under 30 ms p95.

---

## 5. Runtime Request Pipeline

The runtime is an explicit **pipeline of stages** sharing a request context. Each stage is a small, testable unit with a single responsibility, mapping 1:1 to PRD §29.

```text
 ┌──────────┐  ┌───────────┐  ┌───────────┐  ┌─────────┐  ┌───────────┐
 │ Ingress  ├─►│ Token     ├─►│ Resolve   ├─►│ AuthZ   ├─►│ Limits    │
 │ (limits, │  │ validate  │  │ Project / │  │ policy  │  │ (Redis    │
 │  reqID)  │  │ + identity│  │ Env / App │  │ (CEL)   │  │  Lua)     │
 └──────────┘  └───────────┘  └───────────┘  └─────────┘  └─────┬─────┘
                                                                 ▼
 ┌────────┐  ┌────────────┐  ┌───────────┐  ┌────────────┐  ┌──────────┐
 │ Output │◄─┤ Agent loop │◄─┤ Prompt +  │◄─┤ Input      │◄─┤ Cache    │
 │ guard  │  │ model+tools│  │ context   │  │ guardrails │  │ lookup   │
 │ +schema│  │            │  │ +memory   │  │            │  │          │
 └───┬────┘  └────────────┘  └───────────┘  └────────────┘  └──────────┘
     ▼
 ┌────────────────────┐
 │ Respond (SSE/JSON) │──► async: usage + logs + limit reconciliation
 └────────────────────┘
```

**Design rules**

* Stages implement one interface (`Stage.Handle(ctx, *ReqState) error`). New behavior (a new guardrail, a new limit type) is a new stage or plugin, not a change to existing ones.
* Every stage emits an OpenTelemetry span and a structured result recorded in the request log.
* Cancellation flows through `context.Context`: when the client disconnects, the provider call and any in-flight tool calls are cancelled, so no cost is wasted.
* Failures map deterministically to the PRD error taxonomy (401/403/429/502/503/504).

---

## 6. Agent Loop and Tool Engine

The core of the runtime is a bounded loop:

```text
loop (max depth N):
   call model with (prompt + context + tool declarations)
   if model returns final answer      → break
   for each requested tool call:
        1. tool authorization           (CEL policy on identity)
        2. inject identity bindings     (from token, never from model)
        3. validate args vs JSON Schema (model-controlled fields only)
        4. confirmation needed?         → suspend (see §6.2)
        5. execute via Executor         (HTTP / webhook / MCP / SQL template / client-side)
        6. cap size, redact, transform, wrap as UNTRUSTED data
   append tool results → next iteration
```

### 6.1 Executors (pluggable)

```go
type Executor interface {
    Execute(ctx context.Context, call ToolCall, id Identity) (ToolResult, error)
}
```

| Executor | Location | Notes |
|---|---|---|
| HTTP / Webhook | Server | Hardened egress client (SSRF guard), timeouts, retries, circuit breaker per tool, bulkhead (max concurrent calls per tool) |
| MCP | Server | Long-lived client sessions pooled per tool config |
| SQL template | Server | Parameterized read-only templates only; separate connection pool and read-only role |
| Client-side | Device | Emitted to the client over SSE, result returned via `/tool-results` |
| Plugin | Server | Go interface / out-of-process gRPC later |

**Downstream auth modes** (`forward_user_token`, `token_exchange`, `service_token`, `signed_headers`) are implemented as small `Authenticator` strategies attached to executors, keeping executors free of auth logic.

### 6.2 Suspending for confirmation and client-side tools

A request that needs a human confirmation or a device action cannot hold a connection open indefinitely. Design:

1. Runtime serializes the loop state (messages so far, pending tool call, snapshot version).
2. State is stored in Redis with a **5-minute TTL**, encrypted, and bound to `(project, issuer, subject, request_id)`.
3. The stream emits a `confirmation` or `tool_call` event carrying a **signed, single-use continuation token** and closes.
4. The client posts `/confirm` or `/tool-results` with the token; **any** runtime instance can resume it (state is in Redis, so no sticky sessions).
5. On resume, the server re-validates the user's JWT, the token binding, and args integrity before executing.

This keeps instances stateless while supporting human-in-the-loop flows.

### 6.3 Safety invariants (enforced in code, tested continuously)

1. Identity used for data access **only** comes from the validated token.
2. Tool results are **untrusted data**; they cannot change authorization, bindings, or tool availability.
3. Write tools require confirmation by default; confirmation binds exact arguments.
4. Bounded loop depth, per-tool timeout, per-request total time and token budget.
5. Every tool call is logged (name, status, latency; payload only if enabled).

---

## 7. Identity, Policy, and Limits

### 7.1 Identity

* JWKS documents are cached per issuer with background refresh and key-rotation handling. **Token validation is purely local CPU work** (no network call on the hot path).
* Signature, `exp`/`nbf`, `iss`, `aud`, and allowed algorithms are checked. Revocation is out of scope by design (PRD §7.4): Cortexbase forwards and validates but does not manage token lifecycle.
* Output is the normalized `Identity{project, env, issuer, subject, audience, claims}`.

### 7.2 Policy engine

* Policies are compiled to CEL programs in the snapshot; evaluation is microseconds.
* Optional **external policy check** (webhook/OPA) runs with tight timeout, configurable failure mode, and a short-TTL decision cache keyed by identity and policy.

### 7.3 Limits (atomic and distributed)

* Counters live in Redis; each check is **one Lua script call** (sliding-window or token bucket) evaluating all configured windows atomically.
* Key: `lim:{project}:{env}:{issuer_hash}:{subject_hash}:{resource}:{window}`, with a Redis hash tag so multi-key scripts stay on one cluster slot.
* Token limits: **reserve** estimated tokens up front, **reconcile** with actual usage after the response (async), crediting or debiting the difference.
* Store unavailable → behavior per config (`fail_closed` default). Postgres-backed counters exist as a lower-throughput fallback for small installs.
* Concurrency limits use a Redis-backed counter with per-request lease and TTL to recover from crashed instances.

---

## 8. Provider Layer

```text
Runtime ──► ProviderAdapter (interface)
                ├── LiteLLMAdapter        (default: broad provider coverage, fallbacks, cost)
                └── OpenAICompatAdapter   (native Go: high-volume, low-latency path)
```

* Logical model (`fast`) → deployment → adapter, resolved from the snapshot.
* **LiteLLM runs as N stateless replicas** behind an internal load balancer; provider keys live only there (or in Cortexbase secrets for the native adapter).
* Circuit breaker and health checks per deployment; provider errors normalized to the taxonomy (`502 provider_error`).
* Streaming is passed through with **no intermediate buffering** except when `stream_mode: buffered` guardrails demand it.
* Fallback ordering (v0.2): same logical model can list several deployments; adapter layer retries next deployment on eligible errors, before the first token is sent.

---

## 9. Data Architecture

### 9.1 PostgreSQL (durable state)

```text
Control data (low volume, strongly consistent)
  projects, environments, admin_users, roles, audit_log
  ai_apps, ai_app_versions
  prompts, prompt_versions
  policies, policy_versions
  tools, tool_versions, tool_secrets (encrypted)
  models, provider_configs (encrypted credentials), routing_versions
  guardrails, guardrail_versions

Operational data (high volume, append-heavy)
  request_logs   (partitioned by day/week; metadata only by default)
  usage_events   (partitioned; aggregated into usage_rollups)
  usage_rollups  (per project/app/user/model/hour or day)

Optional context data
  threads, messages (TTL)
  memories (pgvector), namespaced by project+issuer+subject+app
```

Conventions:

* `project_id` and `environment` on every tenant-scoped table; RLS as defense in depth.
* **Immutable versions:** published prompt/policy/tool/config versions are append-only; "active" is a pointer.
* **Partition-based retention:** dropping an expired partition replaces expensive row deletes.
* **Delete-by-subject** indexes exist on the subject hash columns to make GDPR purges fast.

### 9.2 Redis/Valkey (ephemeral state)

| Purpose | Keying | TTL |
|---|---|---|
| Limit counters | `lim:{project}:{env}:{user}:…` | window length |
| Response cache | `rc:{project}:{env}:{scope}:{hash}` (`scope` = user or shared) | per config |
| Tool result cache | `tc:{project}:{env}:{user}:{tool}:{argshash}` | per tool |
| Pending confirmation/tool state | `pend:{project}:{req_id}` | minutes |
| Config revision pub/sub | channel `cfg:{project}` | n/a |
| JWKS / policy decision cache | local memory first, Redis optional | minutes |

Cache keys **always include identity scope** unless a cache is explicitly marked shared and non-user-specific, preventing cross-user data leaks.

### 9.3 Usage and log pipeline (do not write synchronously)

```text
Request completes
     │
     ▼
in-process bounded channel (non-blocking, drop-with-metric on overflow if configured)
     │
     ▼
batch writer: every N events or T ms
     │
     ├── small scale: batched INSERT/COPY into Postgres partitions
     └── large scale: Redis Streams (durable buffer) → consumer → Postgres or ClickHouse
```

* The request path only enqueues; user latency never waits on log storage.
* Rollups (`usage_rollups`) are updated by a background aggregator, so dashboard queries hit small tables.
* Backpressure policy is explicit: for **usage/billing-relevant** events, prefer durable buffering; for verbose debug logs, allow sampling under load.
* Content (prompts/responses/tool payloads) is excluded unless enabled, and passes through redaction rules before storage.

---

## 10. Scaling to Millions of Users

### 10.1 Scaling model

```text
                    Load balancer / ingress (SSE-friendly: no response buffering,
                                 long idle timeouts, HTTP/2)
                                   │
         ┌─────────────────────────┼─────────────────────────┐
         ▼                         ▼                         ▼
   Runtime pod 1             Runtime pod 2   …         Runtime pod N      (stateless, HPA)
         │                         │                         │
         └───────────┬─────────────┴──────────────┬──────────┘
                     ▼                            ▼
              Redis Cluster                 LiteLLM replicas  ─►  Providers
                     │
                     ▼
        Postgres (primary + read replica, PgBouncer)  ──► optional ClickHouse
```

| Layer | Scale method |
|---|---|
| Runtime | Add pods; autoscale on CPU and **concurrent open streams** (expose as a metric). No sticky sessions required. |
| LiteLLM | Add replicas; or route high-volume models through the native adapter |
| Redis | Vertical first; Cluster with hash-tagged per-user keys when needed |
| Postgres | PgBouncer; read replica for dashboard and rollup queries; partitioning; move usage to ClickHouse when volume demands |
| Providers | Multiple deployments/keys per logical model, rate-limit aware routing, fallback |

### 10.2 Likely bottlenecks (in order) and mitigations

1. **Provider rate limits and cost.** Multiple API keys/deployments per logical model, fallbacks, response and semantic caching, per-user quotas (already core to the product).
2. **LiteLLM throughput.** More replicas; native OpenAI-compatible adapter for hot models.
3. **Redis hot keys / throughput.** Per-user keys distribute naturally; pipeline/Lua single round-trip; cluster.
4. **Log/usage write volume.** Async batching, Streams buffer, partitioning, ClickHouse.
5. **Postgres connections.** PgBouncer; runtime holds few connections since hot path is Postgres-free.

### 10.3 Connection and memory discipline

* Each SSE stream is a goroutine plus small buffers; budget memory per stream and enforce **max request/response sizes**, **max concurrent streams per instance**, and **per-user concurrency limits** to prevent slow-client exhaustion.
* HTTP server timeouts tuned for streaming (read/header timeouts strict, write timeout per-stream managed manually with heartbeats).
* Send SSE **heartbeat comments** so proxies don't cut idle streams.
* Graceful shutdown: stop accepting, drain in-flight streams up to a deadline, and rely on client SDK retry for interrupted streams.

### 10.4 Multi-region (later)

Runtime pods can run in multiple regions close to users; config snapshots replicate from a single primary control plane (config is small and read-mostly). Counters stay regional by default (a user's limit is per-region unless a global counter is configured, a documented trade-off).

---

## 11. Availability and Failure Modes

| Failure | Behavior |
|---|---|
| Control plane down | Runtime keeps serving from in-memory snapshots; config edits unavailable |
| Postgres down | Runtime serves; usage/log events buffer (bounded) and drain on recovery; memory features degrade |
| Redis down | Limits switch to Postgres fallback if configured, otherwise **fail closed** (default) returning `503`; caches skipped; pending confirmations unavailable |
| LiteLLM down | Failover to other deployments/native adapter if configured, else `503 unavailable` |
| Provider error/timeouts | Retry/fallback before first token; after first token, emit `error` event and stop |
| Tool endpoint slow/down | Per-tool timeout → `504 tool_timeout` to the model as tool error (model may continue); circuit breaker opens to protect the endpoint |
| Bad config published | Validation and compile happen in control plane before publish; runtime rejects an uncompilable snapshot and keeps the last good one; one-click rollback to prior version |
| Instance crash mid-stream | Client SDK retries; usage for the partial request is best-effort recorded, limits reconciled on TTL |

---

## 12. Security Architecture

* **Trust boundaries:** (1) internet ↔ runtime, (2) runtime ↔ implementor backends, (3) runtime ↔ providers, (4) control plane ↔ admins. Each boundary has authentication, validation, and logging.
* **Secrets:** provider credentials and tool secrets are envelope-encrypted at rest with a pluggable `KeyProvider` (env, file, cloud KMS, Vault); decrypted only in memory in runtime instances that need them; never returned by any API.
* **Untrusted inputs:** user messages, tool results, client-side tool results, and retrieved memory are all treated as untrusted and pass through guardrails.
* **SSRF containment** for tool executors (default deny private ranges, DNS-rebinding-safe dialer, allowlists).
* **Admin plane hardening:** separate listener/port, RBAC, audit log, optional network restriction (IP allowlist / private network).
* **Tenant isolation:** `project_id` scoping in every query, cache key, and counter key; automated tests assert no cross-project leakage.
* **Abuse controls:** per-IP and per-project ingress rate limits in addition to per-user limits, body size limits, and payload validation before any expensive work.
* **Token lifecycle** is deliberately outside Cortexbase (implementor's IdP), consistent with PRD §7.4.

---

## 13. Extensibility Points (keeps the core small)

Everything below is an interface with a built-in default, so features are added without touching the pipeline:

| Interface | Built-in implementations |
|---|---|
| `AuthProvider` | Generic JWT, OIDC, Firebase, Supabase |
| `PolicyEvaluator` | CEL local, webhook, OPA |
| `LimitStore` | Redis (Lua), Postgres |
| `ProviderAdapter` | LiteLLM, OpenAI-compatible |
| `Executor` | HTTP, webhook, MCP, SQL template, client-side |
| `Authenticator` (downstream) | forward token, token exchange, service token, signed headers |
| `MemoryProvider` | pgvector, implementor-hosted HTTP, (Redis, Qdrant later) |
| `Guardrail` | size, PII, injection, schema, custom webhook |
| `UsageStore` | Postgres, ClickHouse |
| `KeyProvider` | env/file, KMS, Vault |
| `CacheStore` | Redis, in-memory |

Plugins beyond built-ins start as **webhook-style HTTP plugins** (language-agnostic, safe by isolation) before considering in-process or gRPC plugins.

---

## 14. Code Structure (Go module layout)

Package boundaries enforce the modular monolith. Dependencies flow **inward**: adapters depend on core interfaces; core never imports adapters.

```text
server/
├── cmd/cortexbase/            # main: role selection, wiring
├── internal/
│   ├── core/                  # domain types + interfaces (Identity, Snapshot, ProviderAdapter, …)
│   ├── runtime/               # pipeline stages, agent loop, SSE, request state
│   │   ├── stages/
│   │   ├── agent/
│   │   └── stream/
│   ├── control/               # management API handlers, RBAC, audit
│   ├── identity/              # JWT/OIDC/Firebase/Supabase providers, JWKS cache
│   ├── policy/                # CEL compile/eval, external evaluator
│   ├── limits/                # Redis Lua + Postgres limit stores, token reconciliation
│   ├── prompts/               # templating, versions
│   ├── routing/               # logical model resolution, rules
│   ├── tools/                 # tool engine, executors, downstream auth, SSRF guard
│   ├── guardrails/
│   ├── memory/                # MemoryProvider + pgvector impl
│   ├── cache/
│   ├── providers/             # litellm/, openaicompat/
│   ├── snapshot/              # compile + hot swap + sync
│   ├── usage/                 # event pipeline, rollups
│   ├── store/                 # sqlc queries, migrations, repositories
│   ├── secrets/               # envelope encryption, KeyProvider
│   ├── telemetry/             # otel, metrics, slog setup
│   └── config/                # server config (env/file)
└── testdata/
```

> **SDKs are in separate repositories** (`cortexbase-sdk-kmp`, `cortexbase-sdk-ios`, `cortexbase-sdk-web`) and are not part of this codebase. See `techstack.md §12` for SDK repo layouts and the OpenAPI contract release flow.

**Maintainability rules**

* Interfaces defined where they are **used** (core), implemented in leaf packages.
* No package imports `control` or `runtime` except `cmd`.
* Enforced by `depguard`/import-boundary lint in CI.
* SQL lives in `.sql` files consumed by `sqlc`: reviewable, no ORM magic.
* Every stage and executor has table-driven unit tests plus at least one integration test via testcontainers.
* API changes start in `api/openapi.yaml`; generated code and SDK conformance tests must pass.

---

## 15. Deployment Topologies

### 15.1 Tier 0: Single node (`docker compose up`)

```text
┌──────────────────────── Docker Compose ─────────────────────────┐
│ cortexbase (--role=all)  │ postgres │ valkey │ litellm │ (ollama)│
└─────────────────────────────────────────────────────────────────┘
```

Dashboard is embedded in the `cortexbase` container. One command, a few hundred MB of RAM.

### 15.2 Tier 1: Small production

* 2+ `cortexbase --role=runtime` behind a load balancer; 1 `--role=control`.
* Managed Postgres and Redis; 2+ LiteLLM replicas.
* TLS terminated at the LB; SSE-safe timeouts.

### 15.3 Tier 2: Large scale (Kubernetes)

* Separate Deployments for runtime (HPA) and control; Helm chart.
* Redis Cluster; Postgres primary + replica + PgBouncer; optional ClickHouse.
* Native OpenAI-compatible adapter for high-volume models; LiteLLM for the long tail.
* OpenTelemetry Collector, Prometheus, Grafana, alerting on SLOs.

The **same image and config schema** are used across tiers. Scaling up is a topology change, not a rewrite.

---

## 16. Observability and SLOs

Suggested SLIs/SLOs for the runtime plane:

| SLI | Target |
|---|---|
| Cortexbase-added overhead, p95 (excluding provider/tool time) | < 30 ms |
| Time to first streamed byte overhead, p95 | < 50 ms |
| Availability of runtime API (excluding provider failures) | 99.9% |
| Config propagation (publish → all runtimes) | < 5 s p95 |
| Usage event loss (billing-relevant) | ~0 (durable buffer) |

Key metrics: requests, latency by stage, active streams, limit rejections, policy denials, guardrail blocks, provider/tool error rates and latency, cache hit rate, snapshot revision lag, log pipeline queue depth. Trace every request end to end with the request ID propagated to tool and provider calls.

---

## 17. Key Sequence: Streaming Chat With a Tool Call

```text
Client        Runtime                 Redis        Implementor API     LiteLLM/Provider
  │  POST /stream (JWT)  │                │                │                  │
  ├─────────────────────►│ validate JWT (local)            │                  │
  │                      │ resolve snapshot, AuthZ (CEL)   │                  │
  │                      ├── limit check (Lua) ───►│       │                  │
  │                      │◄────────── ok ──────────┤       │                  │
  │                      │ input guardrails, build prompt  │                  │
  │                      ├──────────────── chat(stream) ──────────────────────►│
  │                      │◄──────────── tool_call get_orders(limit=5) ─────────┤
  │                      │ tool authZ → bind user_id from JWT → validate args  │
  │                      ├── GET /users/{sub}/orders (forwarded JWT) ─►│       │
  │                      │◄──────────────── orders ────────────────────┤       │
  │                      │ cap/redact/wrap as untrusted data          │       │
  │                      ├──────────────── continue chat ─────────────────────►│
  │◄── SSE delta …───────┤◄──────────────── tokens ───────────────────────────┤
  │◄── SSE usage, done ──┤                                                    │
  │                      ├── enqueue usage/log event (async) ──► batch writer ─► Postgres
  │                      ├── reconcile token counters ────────►│
```

---

## 18. Roadmap Alignment

| PRD version | Architectural work |
|---|---|
| **v0.1** | Two-role binary; snapshot sync; identity + CEL policies; Redis/Postgres limits; LiteLLM adapter; SSE streaming; async usage pipeline; admin auth; config export/import |
| **v0.2** | Tool engine and executors (HTTP/webhook), identity bindings, downstream auth, confirmation/continuation state; guardrails and stream modes; native OpenAI-compatible adapter; fallbacks; OTel/Prometheus; privacy operations |
| **v0.3** | MCP and SQL-template executors; memory and threads; semantic cache; external policy check; ClickHouse `UsageStore`; Helm chart |
| **Later** | Multi-region, organizations/teams, agent workflows, out-of-process plugins |

---

## 19. Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Config snapshot drift between instances | Revision numbers, polling backstop, revision-lag metric and alert |
| Redis dependency on hot path | Postgres fallback, explicit fail-closed/open config, health checks |
| LiteLLM as bottleneck/SPOF | Replicas, adapter abstraction, native adapter path |
| Tool endpoints abused for SSRF or data exfiltration | Egress guard, allowlists, identity bindings, result caps and redaction, audit logs |
| Cross-user data leakage via caches/memory | Identity-scoped keys enforced centrally, leakage tests in CI |
| Prompt injection via tool data | Untrusted-data wrapping, output guardrails, tool authorization independent of model text |
| Modular monolith decays into a big ball | Import-boundary lint, interface-first design, ADRs |
| Log volume growth | Metadata-only default, sampling, partitioning, ClickHouse option |

---

## 20. Open Architectural Questions

1. Should the control plane and runtime **share** the same Postgres in Tier 2, or should runtime read config from a replicated read-only store?
2. Global vs per-region limit counters for multi-region deployments.
3. Native adapter scope: which providers get first-class direct adapters beyond OpenAI-compatible?
4. Plugin model: HTTP webhooks only, or also WASM for in-process sandboxed extensions?
5. Whether to offer an optional **egress proxy** component for hardened tool networking in regulated environments.
