# Architecture & Call Flows

Beavend is architected as a **modular monolith** with a strict operational split between two distinct planes: the **Runtime Plane** (data plane) and the **Control Plane** (management plane).

---

## 1. System Overview & Planes

```mermaid
flowchart TD
    subgraph Clients["Client Layer"]
        Mobile["Mobile / Web Apps"]
        Admin["Admin Browser"]
    end

    subgraph RuntimePlane["Runtime Plane (Data Plane) - High Volume / Stateless"]
        Ingress["HTTP / SSE Ingress"]
        SnapCache[("In-Memory Config Snapshot<br/>(atomic.Pointer)")]
        Auth["Identity Verifier & Claims Normalizer"]
        CEL["CEL Policy Engine"]
        Limits["Rate Limiter & Quota (Redis Lua)"]
        Router["Model & Deployment Resolver"]
        AgentLoop["Bounded Agent Loop"]
        SSEWriter["SSE Stream Flusher"]
    end

    subgraph ControlPlane["Control Plane (Management Plane) - Durable / Low Volume"]
        AdminAPI["Admin REST Handlers"]
        Dashboard["Embedded React Dashboard SPA"]
        Compiler["Snapshot Compiler"]
        Audit["Audit Logger"]
    end

    subgraph Storage["State & External Systems"]
        PG[("PostgreSQL 16 + pgvector<br/>(Durable Config, Memory, Rollups)")]
        Redis[("Valkey / Redis<br/>(Pub/Sub Sync, Limits, Suspended States)")]
        ExtAPIs["Implementor APIs / DBs / MCP"]
        LLMs["AI Providers<br/>(Gemini, OpenAI, Claude, Ollama)"]
    end

    Mobile -->|User JWT| Ingress
    Admin -->|Admin Session| AdminAPI
    AdminAPI --> Compiler
    Compiler -->|Durable State| PG
    Compiler -->|Publish Revision| Redis
    Redis -.->|Revision Notification| SnapCache

    Ingress --> Auth
    Auth --> CEL
    CEL --> Limits
    Limits --> Router
    Router --> AgentLoop
    SnapCache -.->|Zero-DB Config| CEL
    SnapCache -.->|Zero-DB Config| Router
    SnapCache -.->|Zero-DB Config| AgentLoop
    AgentLoop -->|Live Data| ExtAPIs
    AgentLoop -->|Model Requests| LLMs
    AgentLoop --> SSEWriter
    SSEWriter -->|Stream Events| Mobile
```

---

## 2. The Zero-DB Request Hot Path

The foundational architectural invariant of Beavend is that **the runtime request path never queries PostgreSQL for configuration**. 

### How Config Snapshots Work:

1. **Durable Edits in Control Plane**: When an admin updates an AI App, prompt template, model routing rule, or CEL policy in the dashboard, the change is written to PostgreSQL as an immutable, versioned record.
2. **Pub/Sub Invalidation**: The control plane publishes a lightweight revision message (`{"project_id": "...", "revision": 42}`) over Redis pub/sub (with a Postgres `LISTEN`/`NOTIFY` fallback).
3. **Pre-Compilation**: Upon receiving the notification, every runtime instance fetches the latest configuration and compiles it into an immutable in-memory `Snapshot`:
   * Google CEL expressions are compiled into executable `cel.Program` bytecodes.
   * Tool arguments are compiled into validated JSON schemas.
   * Prompt templates are parsed and cached.
4. **Atomic Pointer Swap**: The compiled snapshot is atomically swapped into memory using Go's `atomic.Pointer[*Snapshot]`. Active in-flight requests retain their initial snapshot reference to prevent mid-request configuration drift.

```mermaid
sequenceDiagram
    autonumber
    participant Admin as Admin Dashboard
    participant Control as Control Plane
    participant DB as PostgreSQL
    participant PubSub as Redis Pub/Sub
    participant Runtime as Runtime Instance

    Admin->>Control: PUT /v1/admin/apps/{id} (Update Config)
    Control->>DB: INSERT immutable app version
    Control->>DB: UPDATE project revision = revision + 1
    Control->>PubSub: PUBLISH revision_channel {project_id, revision}
    PubSub-->>Runtime: Message: {project_id: "p1", revision: 12}
    Runtime->>DB: Fetch snapshot bundle
    Note over Runtime: Compile CEL, JSON schemas, templates
    Runtime->>Runtime: atomic.Pointer.Store(&newSnapshot)
    Note over Runtime: Zero DB reads for subsequent requests!
```

---

## 3. Runtime Request Call Flow (AST Synthesis)

Extracted from Beavend's AST analysis via **Graphify**, the execution lifecycle of a client AI chat or streaming request follows a deterministic pipeline managed by `ReqState`:

```mermaid
flowchart TD
    A["Client Request: POST /v1/apps/{id}/chat or /stream"] --> B["1. Ingress & Context Initialization (ReqState)"]
    B --> C["2. Identity Validation (JWKS Cache & Provider Normalizer)"]
    C --> D["3. Authorization (In-Memory CEL Policy Evaluation)"]
    D --> E["4. Rate Limiting & User Quotas (Redis Lua Scripts)"]
    E --> F["5. Model Resolution (Primary & Fallback Deployment Resolver)"]
    F --> G["6. Prompt Resolution & System Injection"]
    G --> H["7. Context & Memory Retrieval (pgvector Threads)"]
    H --> I["8. Bounded Agent Loop (Tool Engine & Confirmation Guard)"]
    I --> J["9. Provider Invocation (LiteLLM or OpenAI-Compatible)"]
    J --> K["10. SSE Stream Delivery & Client Cancellation Monitor"]
    K --> L["11. Asynchronous Usage & Cost Aggregator (Bounded Channel)"]
```

### Key Hubs (God Nodes) Identified by Graphify:

* **`Snapshot` & `NewSnapshot()`**: Central architectural hub maintaining all pre-compiled structures, eliminating database overhead.
* **`ReqState`**: Thread-safe pipeline carrier propagating project ID, validated identity, snapshot pointer, telemetry contexts, and active tool states across stages.
* **`Resolver`**: Manages logical model translation, deployment weights, and automatic failovers across provider adapters.
* **`ProviderAdapter`**: Common interface decoupling Beavend runtime logic from underlying LLM protocols (LiteLLM, native OpenAI-compatible, Ollama).
* **`SafeDialer`**: Custom SSRF-safe network transport guarding all external tool HTTP requests.

---

## 4. Bounded Agent Loop & Human Confirmation

When tools are enabled for an AI App, Beavend's runtime runs a bounded execution loop:

1. **Loop Depth Boundary**: The agent loop is strictly capped by `max_depth` (default: 5 iterations) to prevent runaway costs or infinite tool recursion.
2. **Side-Effect Detection**: Tools marked with `side_effect: write` default to `confirmation: required`.
3. **Stateless Freezing**: When confirmation is triggered:
   * Execution state is serialized and stored in Redis under `pend:{project}:{req_id}` with a 5-minute TTL.
   * A cryptographically signed HMAC confirmation token is returned to the client binding `{req_id, tool_id, args_hash, user_subject}`.
   * An SSE `confirmation` event is emitted, and the connection closes.
4. **Stateless Resumption**: The client reviews the tool parameters and calls `POST /v1/apps/{id}/confirm`. Any runtime instance in the cluster can verify the HMAC signature, retrieve the frozen state from Redis, execute the tool, and complete the response.
