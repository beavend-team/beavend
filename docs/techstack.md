# Beavend — Tech Stack

> Companion to `Beavend-PRD.md` and `architecture.md`.
> Goal: the most **efficient** (runtime cost, latency, scale to millions of users) and **effective** (easy to build, operate, and contribute to) stack for a self-hosted AI application backend.
>
> Versions are intentionally not pinned here. Pin the current stable release of each dependency at implementation time and record it in `go.mod`, `package.json`, and `docker-compose.yml`.

---

## 1. Workload Profile (what the stack must be good at)

The stack is chosen for what Beavend actually does, not for general preference.

| Characteristic | Implication |
|---|---|
| Mostly **I/O-bound proxying**: client → Beavend → provider/tool | Needs cheap, massive concurrency, not raw CPU speed |
| **Long-lived streaming (SSE)** responses, seconds to minutes each | Needs efficient connection handling and no response buffering |
| Hot path is **small logic**: validate JWT, evaluate policy, check limit, render prompt | Must be fast and allocation-light; **no DB read per request** |
| **Many concurrent users**, bursty | Stateless horizontal scaling |
| **Self-hosted** by small teams | Single small binary, low memory, trivial Docker/compose, few moving parts |
| **Open source** | Popular, readable language with a large contributor pool |
| Config-heavy, security-sensitive | Strong typing, mature crypto/JWT/JSON Schema libraries |

**Capacity reality check (illustrative assumptions, not measurements).** "Millions of users" almost never means millions of *simultaneous* requests.

```text
1,000,000 daily active users × 10 AI requests/day  = 10M requests/day
Average                                            ≈ 115 requests/sec
Peak (assume 10× average)                          ≈ 1,000–1,500 requests/sec
Average stream duration ~10 s → concurrent streams ≈ 10,000–15,000 at peak
```

That load is comfortably handled by a small cluster of modest Go instances. In practice the limiting factors are **provider rate limits, cost, Redis/Postgres write throughput, and LiteLLM**, not the Beavend server language. The stack and architecture are designed so each of those can be scaled or bypassed independently. Validate with load tests early (§11).

---

## 2. Decision Summary

| Layer | Choice |
|---|---|
| **Server language** | **Go** |
| HTTP / streaming | Go standard library `net/http` (SSE) + `chi` router |
| Primary database | **PostgreSQL** |
| Vector / memory (v0.3) | **pgvector** in PostgreSQL |
| Hot state (counters, cache, pub/sub) | **Redis-compatible** (Valkey or Redis), with Postgres fallback |
| Provider layer | **LiteLLM** (default) behind a `ProviderAdapter`, plus native Go adapter for OpenAI-compatible endpoints |
| Policy / expression engine | **CEL** (Common Expression Language) |
| JWT / OIDC | `lestrrat-go/jwx` (JWKS, JWT validation) |
| Schema validation | JSON Schema (`santhosh-tekuri/jsonschema`) |
| DB access & migrations | `pgx` + `sqlc` + `goose` |
| Usage/log pipeline | In-process batching → Postgres (partitioned); optional **ClickHouse** at large scale |
| Async messaging (optional at scale) | Redis Streams first; NATS JetStream if needed |
| Dashboard | **React + TypeScript + Vite**, Tailwind + shadcn/ui, TanStack Query |
| API contract | **OpenAPI 3.1** as the single source of truth |
| Android/KMP SDK | Ktor client + kotlinx.serialization + coroutines/Flow |
| iOS SDK | Swift native: `URLSession` + `AsyncStream`, no KMP dependency |
| Web SDK | TypeScript (`fetch` + `ReadableStream`), zero dependencies, built with `tsup` |
| Observability | OpenTelemetry (traces), Prometheus (metrics), structured JSON logs (`slog`) |
| Packaging | Single static binary; multi-arch Docker image; Docker Compose; Helm chart later |
| CI/CD | GitHub Actions, `golangci-lint`, `govulncheck`, Trivy, release via GoReleaser |
| Docs | Astro Starlight (or Docusaurus) |

---

## 3. Server Language: Go

### 3.1 Why Go

* **Concurrency model fits exactly.** Goroutines make tens of thousands of open SSE streams cheap, with simple blocking-style code instead of async/await or callback complexity.
* **Efficient.** Low memory per connection, fast startup, predictable GC pauses, small footprint. Runs well on a $5–20 VPS for small installs and scales linearly in clusters.
* **Easiest to operate.** One static binary, no runtime to install, tiny container images. This directly serves the "docker compose up" goal.
* **Easy to maintain.** Small language, one obvious way to do things, fast compiles, excellent tooling (formatter, vet, race detector, pprof built in). New contributors become productive quickly.
* **Ecosystem fits.** First-class libraries for HTTP, JWT/JWKS, OpenTelemetry, Prometheus, pgx (Postgres), Redis, CEL, and JSON Schema. The infrastructure category Beavend belongs to (proxies, gateways, backends like Supabase pieces, Traefik, Caddy, Vault) is heavily Go, so the open-source contributor pool matches.
* **Streaming is native.** `http.Flusher` and context cancellation give proper backpressure and client-disconnect propagation to providers, saving cost when users cancel.

### 3.2 Alternatives considered

| Option | Verdict | Reason |
|---|---|---|
| **Rust** (axum/tokio) | Rejected for v1 | Best raw efficiency, but slower development, harder to hire/onboard contributors, and Beavend is I/O-bound so the gain is marginal. Revisit for isolated hot components only. |
| **TypeScript** (Node/Bun/Deno) | Rejected | Good developer familiarity and shared types with the dashboard, but single-threaded event loop needs more care for CPU spikes (JSON schema, guardrails), higher memory per instance, and heavier runtime to ship. |
| **Kotlin/JVM** (Ktor/Spring) | Rejected | Attractive for sharing code with the KMP SDK and for Android-centric teams, but higher memory, slower cold start, larger images, and JVM tuning burden hurt small self-hosters. Kotlin's real benefit (shared models) is achieved better through OpenAPI codegen. |
| **Python** (FastAPI) | Rejected | Simple and matches LiteLLM, but GIL and per-process memory limit high-concurrency streaming, requiring many workers. Fine for LiteLLM's role, not for the hot path. |

**When to reconsider:** if the core team has no Go experience and cannot acquire it, Kotlin (Ktor) is the next most reasonable option. That is a staffing decision, not a technical one; on technical merit for this workload Go wins.

### 3.3 Go library choices

| Concern | Library | Notes |
|---|---|---|
| HTTP server | `net/http` + `chi` | Stdlib-first, minimal magic, middleware chain |
| JWT/JWKS | `lestrrat-go/jwx` | Robust JWKS caching and key rotation |
| Expressions | `google/cel-go` | Safe, sandboxed, non-Turing-complete; ideal for policies and tier rules |
| JSON Schema | `santhosh-tekuri/jsonschema` | Tool input schema and response schema validation |
| Postgres | `jackc/pgx` (pool) | Fast native driver |
| SQL codegen | `sqlc` | Type-safe queries from plain SQL, no ORM |
| Migrations | `goose` (or Atlas) | Versioned SQL migrations |
| Redis | `redis/go-redis` (or `valkey-go`) | Lua scripts for atomic limits |
| Config | `koanf` or stdlib + YAML | Env + file + secrets refs |
| Logging | stdlib `slog` | Structured JSON |
| Telemetry | OpenTelemetry Go SDK, `prometheus/client_golang` | |
| Templating (prompts) | Go `text/template` restricted, or a sandboxed Mustache-style renderer | Variables `{{name}}`; no arbitrary code |
| MCP client | Official MCP Go SDK (or thin custom client) | Tool executor for MCP |
| Encryption | stdlib `crypto` (AES-GCM) with envelope encryption | Master key from env/KMS |
| Testing | stdlib `testing`, `testify`, `testcontainers-go` | Real Postgres/Redis in integration tests |

**Why CEL for policies.** The PRD describes claim-based policies with operators (`eq`, `in`, `gte`, `regex`). Rather than inventing and maintaining a custom evaluator, policies compile to CEL expressions (e.g. `claims.plan in ['pro','team'] && claims.ai_access == true`). CEL is fast, safe (no side effects, bounded), type-checkable at save time, and already understood by the industry. The dashboard can offer a simple builder that emits CEL; power users can write CEL directly. Tier rules for limits use the same engine.

---

## 4. Data Stores

### 4.1 PostgreSQL (system of record)

Holds: projects, environments, AI Apps, prompts, policies, tools (with encrypted secrets), models, guardrails, admin users, audit log, optional memory/threads, and durable usage/log tables.

* Single, ubiquitous, easy to self-host, easy to back up.
* **pgvector** for semantic memory keeps the initial deployment to one database.
* Use **partitioned tables** (by time) for logs/usage to make retention cheap (drop partition) and queries fast.
* `project_id` and `environment` on every tenant-scoped row; optionally Row-Level Security as defense in depth.
* Scale path: managed Postgres, read replica for dashboard queries, PgBouncer.

### 4.2 Redis-compatible store (hot state)

Used for: **rate-limit/quota counters**, response/tool caches, short-lived pending-confirmation state, pub/sub for config invalidation, optional Redis Streams for log buffering.

* Recommend **Valkey** as the default in the compose file (BSD-licensed, Redis-protocol compatible), with Redis supported. This avoids licensing friction for an open-source distribution.
* Atomic limits via **Lua scripts** (sliding window / token bucket) in a single round trip.
* **Postgres fallback** counters for tiny installs that don't want Redis (lower throughput, same semantics).
* Scale path: Redis Cluster / managed Redis; key hashing by `project:issuer:subject` for even distribution.

### 4.3 Analytics at very large scale (optional)

Postgres partitions handle a large volume comfortably. When request logs reach hundreds of millions of rows and the dashboard's aggregate queries slow down, add **ClickHouse** as an optional sink for usage/logs behind the same `UsageStore` interface. It is opt-in, not required for v0.1.

### 4.4 Explicitly not chosen

| Option | Why not |
|---|---|
| MongoDB / NoSQL as primary | Config and relational integrity (versions, references) fit SQL better |
| Kafka | Operationally heavy for the target audience; Redis Streams/NATS cover needs. Reconsider only at extreme scale |
| Separate vector DB for v1 | pgvector is enough; abstraction (`MemoryProvider`) allows Qdrant/Pinecone later |
| ORM (GORM etc.) | `sqlc` gives type safety with explicit SQL and fewer surprises |

---

## 5. Provider Layer

### 5.1 LiteLLM as default

LiteLLM stays as specified in the PRD: provider integrations, unified API, fallbacks, cost data. Beavend talks to it over its OpenAI-compatible HTTP API through the `ProviderAdapter` interface.

Operational rules:

* Run LiteLLM as a **stateless service with multiple replicas** behind an internal load balancer. Its master key and provider keys never leave the server network.
* Beavend, not clients, holds the LiteLLM credential.
* Treat LiteLLM as a **replaceable** component (see below).

### 5.2 Native OpenAI-compatible adapter (scale escape hatch)

LiteLLM is Python and can become the throughput bottleneck at very high request rates. Many providers (OpenAI, OpenRouter, Ollama, vLLM, Azure OpenAI-compatible, Gemini's OpenAI-compatible endpoint) speak the OpenAI wire format.

* Ship a **native Go `OpenAICompatAdapter`** from v0.2 that calls these endpoints directly, skipping LiteLLM for those providers.
* Per-model choice of adapter in configuration: `adapter: litellm | openai_compat`.
* LiteLLM remains the path for exotic providers and its fallback/cost features.

This gives a high-scale path without rewriting anything and keeps the "don't clone LiteLLM" principle: Beavend only speaks the common wire format itself.

### 5.3 Adapter interface (sketch)

```go
type ProviderAdapter interface {
    Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
    Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
    Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
    Health(ctx context.Context) error
}
```

Errors are normalized to Beavend's error taxonomy; usage/cost is returned in a uniform struct.

---

## 6. Dashboard

| Concern | Choice | Reason |
|---|---|---|
| Framework | **React + TypeScript** | Largest ecosystem and contributor pool |
| Build | **Vite** | Fast dev, static output |
| UI | **Tailwind CSS + shadcn/ui (Radix)** | Accessible, unopinionated, easy theming; Supabase-like feel |
| Data fetching | **TanStack Query** | Caching, retries, mutation handling |
| Routing | TanStack Router (or React Router) | Type-safe routes |
| Forms/validation | React Hook Form + Zod | Schemas generated from OpenAPI where possible |
| Charts | Recharts (or ECharts for heavy dashboards) | Usage/latency visualizations |
| Code/schema editors | Monaco editor | Tool schemas, CEL, YAML editing |
| API client | Generated from OpenAPI (`openapi-typescript`) | No hand-written types |

**Deployment:** the built static dashboard is **embedded into the Go binary** (`go:embed`) and served by the control-plane role. One image, no separate frontend container by default. It can still be hosted separately (CDN) if desired.

Rejected: Next.js/SSR (unneeded complexity and a Node runtime for what is an admin SPA), Angular/Vue (smaller contributor overlap for this audience).

---

## 7. SDKs

### 7.1 Strategy: contract first, separate repos

The **OpenAPI 3.1 spec** (plus a documented SSE event schema) is the single source of truth. Server handlers, dashboard types, and SDK models are validated or generated against it. This is the biggest lever for keeping SDKs maintainable across independent repositories.

**Each SDK lives in its own repository.** Mobile SDKs (especially KMP/Android) have complex Gradle setups, their own build variants, publishing pipelines, and CI requirements (emulator/simulator tests, Maven Central signing, XCFramework assembly) that are incompatible with a server monorepo workflow.

#### Repositories

| Repository | Language | Distributes to |
|---|---|---|
| `beavend/beavend` | Go + React | — (server + dashboard) |
| `beavend/beavend-sdk-kmp` | Kotlin Multiplatform | Maven Central (Android) |
| `beavend/beavend-sdk-ios` | Swift | Swift Package Manager |
| `beavend/beavend-sdk-web` | TypeScript | npm |

#### OpenAPI spec distribution

The spec source lives at `api/openapi.yaml` in the main `beavend` repo. On every versioned release (e.g. `v0.2.0`) the CI publishes the spec as a GitHub Release asset:

```
https://github.com/beavend/beavend/releases/download/v0.2.0/openapi.yaml
```

Each SDK repo contains a pinned copy of the spec (`api/openapi.yaml`) checked into version control. A `make update-spec VERSION=v0.2.0` target downloads the pinned version:

```makefile
update-spec:
    curl -fL https://github.com/beavend/beavend/releases/download/$(VERSION)/openapi.yaml \
         -o api/openapi.yaml
```

SDK CI runs contract conformance tests against the committed spec. Updating to a new API version is an explicit, reviewable PR in each SDK repo. This gives clean separation — SDK contributors never need the server codebase — while keeping the contract versioned and auditable.

#### SDK versioning policy

* SDK versions follow SemVer independently of the server version.
* Each SDK release documents the minimum compatible server version (e.g. `beavend >= v0.1.0`).
* The SDK sends `X-Beavend-SDK-Version: <version>` on every request so the server can log and warn on version skew.
* Breaking server API changes are introduced only in minor or major server versions; SDKs have at least one minor version cycle to adapt before the old endpoint is removed.

### 7.2 Kotlin Multiplatform SDK (`beavend-sdk-kmp`) — Android

* Targets: `androidTarget` (primary distribution), with the multiplatform module structure ready to add other targets later.
* **Ktor client** (OkHttp engine on Android), **kotlinx.serialization**, **coroutines/Flow** for streaming.
* Public API: `ai(id).chat/stream`, token provider callback, retries with backoff, cancellation, client-tool handlers, confirmation flow.
* Distributed via **Maven Central**. Publishing pipeline uses the Sonatype Portal with GPG signing.
* Gradle setup: `kotlin("multiplatform")` convention plugin, `build-logic/` for shared build logic, `gradle/libs.versions.toml` version catalog. This is why a separate repo is essential — this Gradle setup conflicts with a Go server workspace.
* Conformance tests run against a mock Beavend server (Docker) in CI.

### 7.3 iOS SDK (`beavend-sdk-ios`) — Swift native

* **Pure Swift** implementation — no KMP dependency. Implements the same thin client against the REST/SSE API using `URLSession` and `AsyncStream`.
* Public API mirrors the KMP SDK surface: `ai(id).chat/stream`, token provider callback, retries, cancellation, client-tool handlers, confirmation flow.
* Idiomatic Swift: `async/await`, `AsyncSequence` for streaming, `Sendable`-conforming types.
* Distributed via **Swift Package Manager** (published as a source package to this repo's tags).
* Minimum deployment target: iOS 16+ (for `AsyncStream` and structured concurrency).
* CI: `xcodebuild` on macOS runners; unit tests + integration tests against a mock server container.

### 7.4 Web SDK (`beavend-sdk-web`) — TypeScript

* Zero runtime dependencies; `fetch` + `ReadableStream` SSE parser; works in browsers, Node, Deno, Bun, edge runtimes.
* Built with **`tsup`** (esbuild-based): produces ESM + CJS dual outputs with `.d.ts` declarations.
* Published to **npm**.
* Full TypeScript types generated from the OpenAPI spec via `openapi-typescript`.

### 7.5 Other languages

Community SDKs (Python, Flutter, React Native) are supported by publishing the OpenAPI spec as a release artifact and a conformance test suite (§11).

---

## 8. Observability

| Signal | Tooling |
|---|---|
| Traces | OpenTelemetry SDK, OTLP export; spans for validate, authorize, limit, guardrails, model, each tool call |
| Metrics | Prometheus endpoint (`/metrics`); RED metrics per AI App, provider, tool; Redis/Postgres pool stats |
| Logs | Structured JSON via `slog`, request ID on every line, sensitive fields redacted by default |
| Dashboards/alerts | Ship example Grafana dashboards and alert rules |
| Profiling | `pprof` behind admin auth |

Beavend's own **usage/log tables** (for the product dashboard) are separate from operational telemetry (for operators).

---

## 9. Security Tooling

* **Secrets:** envelope encryption (AES-256-GCM); data-encryption keys wrapped by a master key from environment, file, or KMS (AWS KMS, GCP KMS, Vault) via a `KeyProvider` interface.
* **Tool egress:** HTTP tool executor uses a hardened client with SSRF protections (block private/link-local/metadata ranges by default, DNS-rebinding-safe dialer, per-project egress allowlist).
* **Supply chain:** `govulncheck`, Dependabot/Renovate, Trivy image scan, SBOM (Syft) and signed images (cosign) on release.
* **Static analysis:** `golangci-lint` (incl. `gosec`), CodeQL.
* **Fuzzing:** Go native fuzzing on JWT parsing, CEL policy inputs, schema validation, SSE parser.

---

## 10. Packaging & Deployment

| Tier | Target | Stack |
|---|---|---|
| **Tier 0: Single node** | Hobby, dev, small teams | Docker Compose: `beavend` (single binary, all roles), `postgres`, `valkey`, `litellm` (+ optional `ollama`) |
| **Tier 1: Small production** | Thousands to low millions of users | 2+ Beavend replicas behind a load balancer, managed Postgres, managed Redis, 2+ LiteLLM replicas |
| **Tier 2: Large scale** | Millions of active users | Kubernetes (Helm chart), separate runtime and control-plane deployments, HPA on CPU/concurrent streams, Redis Cluster, Postgres primary + read replica + PgBouncer, optional ClickHouse, native OpenAI-compatible adapter for high-volume models |

Build/release: GoReleaser produces multi-arch binaries and images; SemVer; migrations run automatically or via `beavend migrate`.

---

## 11. Testing Strategy

| Level | Approach |
|---|---|
| Unit | Policy engine, limit math, identity normalization, routing, redaction |
| Integration | `testcontainers-go` with real Postgres, Redis, and a mock provider |
| Contract | OpenAPI conformance tests run against the server **and** each SDK |
| Security | Confused-deputy tests (model-supplied `user_id` must be ignored), SSRF tests, JWT attack corpus (alg confusion, `none`, wrong audience) |
| Load | `k6` scenarios for streaming: target concurrent streams and p95 overhead (<30 ms excluding provider/tool) from the PRD NFRs |
| Chaos | Kill Redis/LiteLLM/tool endpoint mid-stream; verify degradation matches PRD (fail-closed limits, 503, 504) |
| Mock provider | A deterministic fake LLM server used across all tests and demos |

---

## 12. Repository Layout

SDKs live in their own repos. The main repo contains the server, dashboard, and the API contract.

### `beavend/beavend` — server + dashboard

```text
beavend/
├── api/                 # OpenAPI spec (source of truth), SSE schema, error taxonomy
├── server/              # Go: runtime + control plane (single module)
├── dashboard/           # React + TS admin SPA
├── deploy/
│   ├── docker/          # Dockerfile, compose files
│   └── helm/            # Helm chart (later)
├── docs/                # Starlight/Docusaurus site
├── examples/            # Sample tool backend, curl-based quickstart
├── tools/               # Codegen scripts, mock provider, load tests
└── .github/             # CI workflows
```

### `beavend/beavend-sdk-kmp` — Android (Kotlin Multiplatform)

```text
beavend-sdk-kmp/
├── api/                 # Pinned openapi.yaml (downloaded via make update-spec)
├── build-logic/         # Convention plugins (Gradle)
├── gradle/              # libs.versions.toml version catalog
├── beavend-sdk/      # KMP library module (androidTarget)
│   ├── src/androidMain/
│   └── src/commonMain/
├── beavend-sdk-test/ # Integration tests (mock server via testcontainers)
├── sample/              # Sample Android app
├── Makefile             # update-spec, publish, test targets
└── .github/             # CI: lint, test, publish to Maven Central
```

### `beavend/beavend-sdk-ios` — Swift native

```text
beavend-sdk-ios/
├── api/                 # Pinned openapi.yaml
├── Sources/
│   └── Beavend/      # Swift source (URLSession, AsyncStream, SSE parser)
├── Tests/
│   └── BeavendTests/ # Unit + integration tests
├── Package.swift        # SwiftPM manifest
├── Makefile             # update-spec, test targets
└── .github/             # CI: xcodebuild on macOS runners
```

### `beavend/beavend-sdk-web` — TypeScript

```text
beavend-sdk-web/
├── api/                 # Pinned openapi.yaml
├── src/                 # TypeScript source
├── dist/                # Built ESM + CJS output (tsup)
├── tsup.config.ts
├── package.json
├── Makefile             # update-spec, build, test targets
└── .github/             # CI: lint, test, publish to npm
```

### API contract release flow

When a new `beavend` version is tagged, the main repo CI:

1. Validates `api/openapi.yaml` against the live server handlers.
2. Publishes `openapi.yaml` as a GitHub Release asset at the tagged version.
3. Opens automated PRs in each SDK repo bumping the pinned spec version — SDK maintainers get a ready-to-review diff.

Each SDK repo pins a spec version explicitly; updating is a deliberate, reviewable PR. SDK contributors never need the server codebase.

---

## 13. Decision Log (ADR summary)

| # | Decision | Status |
|---|---|---|
| 1 | Go for the server | Accepted |
| 2 | PostgreSQL as system of record, pgvector for memory | Accepted |
| 3 | Valkey/Redis for hot state, Postgres fallback | Accepted |
| 4 | LiteLLM default behind `ProviderAdapter`; native OpenAI-compatible adapter for scale | Accepted |
| 5 | CEL for policies and tier rules | Accepted |
| 6 | OpenAPI-first contract, generated clients/types | Accepted |
| 7 | React + Vite SPA embedded in the server binary | Accepted |
| 8 | ClickHouse optional for large-scale analytics | Proposed (post v0.2) |
| 9 | Apache-2.0 license | Accepted |
| 10 | SDKs in separate repos; OpenAPI spec published as release artifact | Accepted |
| 11 | iOS SDK: pure Swift native (`URLSession` + `AsyncStream`), no KMP dependency | Accepted |
| 12 | Web SDK built with `tsup` (ESM + CJS) | Accepted |

Keep one short ADR file per decision in `docs/adr/` as the project evolves.
