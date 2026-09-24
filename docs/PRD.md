# Beavend — Product Requirements Document

> **Version:** 0.2 (revised draft)
> **Tagline:** The self-hosted AI backend for your apps. Bring your users, your data, your models.

---

## 1. Product Overview

Beavend is an open-source, self-hosted AI application backend for mobile, web, and other client applications.

It provides an application-facing AI runtime, similar in developer experience to Firebase AI Logic, while remaining:

* Provider-agnostic
* Self-hostable
* Open-source
* Application-agnostic
* Identity-provider agnostic
* Data-source agnostic (it owns no application data)
* Client SDK friendly
* Configurable through a web dashboard and config-as-code

Beavend sits between an application and AI model providers, and between the AI and the implementor's own data.

```text
Android / iOS / KMP / Web
            │
            │ User Identity Token (JWT)
            ▼
     Beavend Runtime
            │
            ├── Token Validation / Identity
            ├── Authorization / Policies
            ├── User Limits / Quotas
            ├── Prompts
            ├── Model Routing
            ├── Guardrails
            ├── Tools & Data Access ───────► Implementor APIs / Databases / MCP
            ├── Context (conversation, memory)
            ├── Caching
            ├── Usage / Observability
            │
            ▼
     Provider Adapter (LiteLLM by default)
            │
            ├── Gemini
            ├── OpenAI
            ├── Anthropic
            ├── OpenRouter
            ├── Ollama
            └── Other Providers
```

Beavend does not replace LiteLLM as a provider gateway.

```text
Beavend     → Application-facing AI runtime
     ↓
LiteLLM        → Provider abstraction / gateway
     ↓
AI Providers
```

The primary abstraction in Beavend is the **AI App**: a configured AI capability that an application can invoke (e.g. `assistant`, `document-analyzer`, `support-agent`).

The client never needs to know which provider, model, prompt, or tool set is being used:

```kotlin
beavend.ai("assistant").chat { message("Hello") }
```

---

## 2. Terminology

The word "app" is overloaded. Beavend uses these terms strictly:

| Term | Meaning |
|---|---|
| **Implementor** | The developer/team who deploys Beavend and builds an application on it |
| **End user** | A person using the implementor's application |
| **Project** | A tenant in Beavend: one implementor application with its own config, auth settings, and environments |
| **Environment** | `development`, `staging`, `production` within a Project |
| **AI App** | A runnable AI capability inside a Project (e.g. `assistant`) |
| **Tool** | A named, schema-typed capability the model may call |
| **Identity** | Normalized representation of the end user derived from a validated token |
| **Provider** | An AI model provider (Gemini, OpenAI, …) |
| **Logical model** | An implementor-defined alias (`fast`, `reasoning`, `vision`) mapped to a provider deployment |

Never use "app" alone in documentation or UI.

---

## 3. Goals

### 3.1 Primary Goals

Beavend should:

1. Let implementors connect their existing authentication provider.
2. Validate end-user identity tokens and derive a normalized identity.
3. Authorize AI access based on identity and claims.
4. Apply per-user inference limits and quotas.
5. Keep AI provider credentials entirely server-side.
6. Provide configurable AI Apps.
7. Manage prompts centrally and version them.
8. Route requests to appropriate models.
9. Support multiple AI providers.
10. Support **custom tools** that the implementor defines, including tools that fetch live data from the implementor's own APIs and databases.
11. Propagate the end user's identity to those data sources safely.
12. Support guardrails.
13. Support optional conversation state and memory.
14. Support caching.
15. Provide usage tracking and observability.
16. Provide a Supabase/Firebase-style management dashboard.
17. Provide Android, iOS, KMP, and Web SDKs.
18. Be fully self-hostable using Docker.
19. Remain application-agnostic.
20. Spare implementors from building an AI backend from scratch.

---

## 4. Non-Goals

Beavend should NOT:

* Become an authentication provider or issue tokens.
* Replace Firebase Auth, Supabase Auth, Auth0, Clerk, Keycloak, etc.
* **Manage token revocation, sessions, or token lifetime.** These belong to the implementor's identity provider (see §8.4).
* Own the application's users.
* Own application subscription/billing state.
* Own, replicate, or require the implementor's business data.
* Require a specific authentication provider.
* Expose provider API keys to clients.
* Become a clone of LiteLLM.
* Hardcode application-specific concepts such as recipes, children, documents, or orders.
* Require a specific LLM provider.
* Require a specific database for the implementor's application.
* Generate and execute arbitrary LLM-written SQL against implementor databases.

---

## 5. Data Principles

Beavend is **stateless with respect to application data**. It does not own, replicate, or require the implementor's user data.

Beavend stores only:

1. **Configuration:** Projects, AI Apps, prompts, policies, tools, models, guardrails.
2. **Operational data:** usage counters, request logs (metadata only by default).
3. **Optional, opt-in context:** conversation state and memory, with TTL, per-namespace deletion, and a pluggable `MemoryProvider` that can point at the implementor's own store.

Rules:

* Application data is **fetched live at request time** through Tools (or Data Connectors), on behalf of the authenticated end user.
* Fetched data is **not persisted** by default.
* Tool inputs/outputs and prompt/response bodies are **excluded from logs** unless explicitly enabled per AI App, with redaction rules.
* Conversation history is **client-supplied by default** (stateless mode). Server-side threads are optional.
* The implementor's data remains under the implementor's access control. Beavend never widens it.

### 5.1 Three context mechanisms

| Mechanism | Source of truth | Persisted by Beavend? |
|---|---|---|
| Conversation state | Client (default) or server-side thread (opt-in) | Only if opt-in, with TTL |
| Memory | Beavend `MemoryProvider` (opt-in), or implementor's store | Only if opt-in |
| Live data access | Implementor APIs/databases via Tools | No |

---

## 6. Core Concept: AI App

An AI App represents one AI capability exposed by an implementor's application.

```text
AI App: assistant
├── Access policy
├── Limits
├── Prompt (versioned)
├── Logical models + routing rules
├── Tools
├── Guardrails
├── Context (conversation / memory)
├── Response schema (optional)
└── Cache configuration
```

A Project can define many AI Apps, each independently configured:

```text
assistant           model: fast       memory: on    tools: search, get_orders
document-analyzer   model: reasoning  memory: off   tools: none
image-analyzer      model: vision     memory: on
```

The client references only the AI App ID. It must not need to know provider names, model IDs, API keys, system prompts, routing rules, or guardrail configuration.

---

## 7. Authentication

### 7.1 Principle

Beavend does NOT authenticate users itself. Authentication belongs to the implementor's application. Beavend **consumes** an identity token issued by the implementor's identity provider and **validates it**.

Authentication is for the **end user**, not for the AI App.

```text
Implementor's Auth Provider ─► End User ─► JWT ─► Beavend
```

One Beavend deployment serves many end users; each request is evaluated against the identity in that request's token.

### 7.2 Supported authentication models

Initial:

* Generic JWT
* Generic OIDC (issuer + JWKS)
* Firebase Auth
* Supabase Auth

Later: Auth0, Clerk, Keycloak presets (all reachable through generic OIDC).

An authentication-provider abstraction keeps the AI runtime independent of any provider.

```yaml
authentication:
  provider: oidc
  issuer: https://auth.example.com
  audience: [my-application]
  jwks_url: https://auth.example.com/.well-known/jwks.json
```

```yaml
authentication:
  provider: firebase
  project_id: example-project
```

```yaml
authentication:
  provider: supabase
  issuer: https://example.supabase.co/auth/v1
```

### 7.3 What Beavend validates

On each request Beavend verifies, and only verifies:

* Signature (via JWKS or configured key)
* Expiry (`exp`) and not-before (`nbf`), with configurable clock skew
* Issuer (`iss`)
* Audience (`aud`)
* Allowed signing algorithms (reject `none` and unexpected algorithms)

JWKS documents are cached with refresh on key rotation.

### 7.4 Revocation is out of scope

Beavend only **forwards and validates** tokens. It does not issue them, and it does not track or manage their lifecycle.

* Beavend does **not** maintain a denylist, session store, or introspection client.
* A token that is validly signed, unexpired, and matches issuer/audience is accepted.
* Token lifetime, refresh, logout, and revocation are the **implementor's identity provider's responsibility**. Implementors who need fast cutoff should use short-lived tokens at their provider.
* When a tool uses `forward_user_token`, the implementor's own API remains free to reject a revoked or otherwise invalid token, so revocation can still take effect on data access without any Beavend involvement.
* Blocking a specific user from AI features can be done through the implementor's claims (e.g. `ai_access: false` at next token issue) or Beavend policies, not through token revocation.

---

## 8. Identity Extraction

After validation, Beavend normalizes the token into an internal identity:

```text
Identity
├── project
├── environment
├── subject
├── issuer
├── audience
└── claims
```

```json
{
  "subject": "user_123",
  "issuer": "https://auth.example.com",
  "audience": "my-application",
  "claims": { "role": "user", "ai_access": true, "plan": "pro" }
}
```

The stable Beavend user key is `project + issuer + subject`. The identity feeds authorization, limits, memory, usage tracking, logging, and **tool identity bindings**.

---

## 9. Authentication vs Authorization

These stay separate.

* Authentication: *Who is this user?* (token validation)
* Authorization: *Is this user allowed to use this AI App or Tool?* (policy evaluation)

An authenticated user does not automatically have access to every AI App or Tool.

---

## 10. Authorization Policies

Beavend provides generic policies. It does not hardcode concepts such as `premium`, `subscription`, `free`, or `paid`. Implementors expose those through token claims or an external check.

```yaml
policies:
  ai-access:
    require:
      authenticated: true

  advanced-ai:
    require:
      authenticated: true
      claims:
        - { path: ai_access, op: eq, value: true }
        - { path: plan, op: in, value: [pro, team] }
```

**Operators:** `eq`, `neq`, `in`, `not_in`, `gte`, `lte`, `regex`, `exists`, plus `all` / `any` / `not` composition.

**External policy check (optional):** for state that lives only in the implementor's system.

```yaml
policies:
  subscription-check:
    external:
      type: webhook        # webhook | opa
      url: https://api.example.com/beavend/authorize
      timeout_ms: 500
      on_failure: deny     # deny | allow
      cache_ttl: 60
```

AI Apps and Tools reference policies by name:

```yaml
app:
  id: assistant
  access_policy: ai-access
```

---

## 11. User-Level Limits

Limits are evaluated per authenticated user, keyed by `project + issuer + subject`.

```yaml
limits:
  - resource: inference
    window: 1m
    max: 5
  - resource: inference
    window: 24h
    max: 30
  - resource: tokens
    window: 24h
    max: 100000
```

### 11.1 Tiers

Limits can vary by claims, so implementors can express plans without Beavend knowing about billing:

```yaml
limits:
  tiers:
    - when: { claim: plan, op: eq, value: pro }
      rules:
        - { resource: inference, window: 24h, max: 500 }
    - default:
      rules:
        - { resource: inference, window: 24h, max: 30 }
```

### 11.2 Token accounting

Token limits use a **pre-request estimate** (input size plus max output) to admit the request, then **post-request reconciliation** with actual usage.

### 11.3 Counter store

Counters require a store from v0.1: Redis if present, otherwise a PostgreSQL fallback. Behavior when the store is unavailable is configurable (`fail_closed` by default).

### 11.4 Future resources

`tool_calls`, `specific_tool`, `specific_model`, `cost`, `concurrent_requests`.

---

## 12. Security Model

Provider credentials are never distributed to clients. Neither are LiteLLM virtual keys.

```text
Mobile App ── User JWT ──► Beavend
                              ├── validate token
                              ├── authorize AI App
                              ├── check user limits
                              ├── apply guardrails
                              ├── select model
                              ▼
                           LiteLLM ── provider credentials ──► AI Provider
```

A stolen user token does not expose provider credentials. It grants only what that user is entitled to and remains subject to expiry, per-user limits, AI App policies, and tool policies. Token lifecycle is the implementor's identity provider's domain (§7.4).

Additional controls:

* **Project ID plus origin/bundle allowlists** to reduce misuse of a project endpoint.
* **Secrets encrypted at rest** (provider keys, downstream tool credentials); never returned via API.
* **Confused-deputy protection:** user identity used for data access comes only from the validated token, never from model output (§17.3).
* **Tool results are untrusted input** (§17.6).
* Application-specific attestation (e.g. device attestation) remains outside Beavend scope, though claims from it may be used in policies.

---

## 13. Provider Management

The dashboard configures AI providers: Google Gemini, OpenAI, Anthropic, OpenRouter, Ollama, Azure OpenAI, AWS Bedrock, and other LiteLLM-supported providers.

Credentials are stored server-side, encrypted, and never returned to any SDK or API response.

### 13.1 ProviderAdapter

The provider layer is behind a `ProviderAdapter` interface. **LiteLLM is the default implementation, not a hard dependency.** The adapter covers chat completion, streaming, embeddings, usage/cost reporting, and error normalization.

---

## 14. Model Management

Beavend exposes **logical models** to AI Apps: `fast`, `reasoning`, `vision`, `embedding`, `local`.

```yaml
models:
  fast:      { deployment: gemini-... }
  reasoning: { deployment: claude-... }
  vision:    { deployment: gemini-... }
  local:     { deployment: ollama/... }
```

Administrators can change the underlying model without releasing a new client version.

---

## 15. Model Routing

Server-side routing selects the logical model:

```yaml
routing:
  rules:
    - when: { modality: image }
      use: vision
    - when: { complexity: high }
      use: reasoning
    - when: { modality: text }
      use: fast
    - default:
        use: fast
```

Beavend handles application-level routing. LiteLLM handles provider-level routing, retries, and fallback.

---

## 16. Prompt Management

Prompts are managed server-side. An AI App has a system prompt, variables, and a version.

```text
assistant
 ├── v1
 ├── v2
 └── v3 (active)
```

Published versions are **immutable**, enabling rollback, comparison, and traceability of which prompt produced a request. Variables may be filled from static config, request metadata, or identity claims (never from untrusted model output).

---

## 17. Tools & Data Access

### 17.1 Definition

A **Tool** is a named, schema-typed capability the model may call. **Implementors create custom tools** in the dashboard or in config. Tools are how the AI reaches the implementor's real data, since Beavend holds none.

### 17.2 Executor types

| Type | Use |
|---|---|
| HTTP | Call the implementor's API (recommended default) |
| Webhook | Async or fire-and-forget actions |
| MCP | Existing MCP servers |
| SQL template | Parameterized, read-only queries against a connected database |
| Client-side | Declared server-side, executed by the SDK on the device (§19) |
| Custom executor / plugin | Anything else |

### 17.3 Tool definition

```yaml
tool:
  id: get_orders
  description: "Get the current user's recent orders"
  version: 2
  side_effect: read              # read | write
  input_schema:                  # JSON Schema; model-controlled fields only
    type: object
    properties:
      limit: { type: integer, maximum: 20 }
  identity_bindings:             # server-injected, NEVER model-controlled
    user_id: identity.subject
  executor:
    type: http
    method: GET
    url: https://api.example.com/users/{user_id}/orders
    timeout_ms: 5000
    retries: 1
  downstream_auth:
    mode: forward_user_token     # forward_user_token | token_exchange | service_token | signed_headers
  result:
    max_bytes: 20000
    redact: [card_number, ssn]
    transform: "$.orders[*].{id,status,total}"
  authorization:
    policy: authenticated
  confirmation: none             # none | required
  cache: { enabled: false }
```

### 17.4 Data access principles

* **Identity propagation, not credential sharing.** Preferred mode is `forward_user_token`: the implementor's API receives the user's own JWT and enforces its own access rules. Alternatives: token exchange (RFC 8693), signed identity headers (HMAC), or a scoped service token plus a user ID header.
* **Identity bindings** fill `user_id`-style parameters from the validated token. The model cannot override or supply them. This prevents the confused-deputy problem where one user asks the model to read another user's data.
* **Least privilege for databases.** The SQL executor supports only pre-defined parameterized templates (no LLM-generated SQL), using a read-only role, ideally with row-level security keyed on the user ID.
* **Write tools** default to `confirmation: required` and support idempotency keys.
* **Limits:** max tool-call loop depth per request, per-tool timeout, per-tool rate limits.
* **Secrets** for downstream auth are stored encrypted server-side.
* **Caching** of tool results is always scoped by `(project, issuer, subject, tool, args)`.
* **Request correlation:** `X-Request-Id` is propagated to downstream calls.

### 17.5 Tool flow

```text
Model requests get_orders(limit=5)
  → Tool authorization (policy check)
  → Inject identity bindings (user_id from JWT)
  → Validate args against input_schema
  → Call implementor API (forwarded token)
  → Cap size, redact, transform
  → Wrap as untrusted data
  → Return to model
```

### 17.6 Tool results are untrusted

Data returned by tools may contain injected instructions (indirect prompt injection). Tool results are wrapped as data, size-capped, passed through output/injection guardrails, and never allowed to alter tool authorization or identity bindings.

### 17.7 Tool testing and lifecycle

* Dashboard **Test tool** action using a sample identity or pasted token.
* **Dry-run** mode for write tools.
* Tool versions are immutable once published; AI Apps pin a version or track latest.
* Tool health/status and error rates visible in the dashboard.

---

## 18. Tool Authorization & Confirmation

### 18.1 Authorization

Every tool has its own authorization policy, evaluated after the model requests it. Access to an AI App does not imply access to its tools.

```text
AI App authorization → Model requests tool → Tool authorization → allowed | denied → Execution
```

### 18.2 Confirmation

Tools that cause side effects can require explicit user confirmation.

```text
AI: I can create this item for you.   [Cancel] [Create]
```

The server issues a **signed, short-lived confirmation token** bound to the exact tool, arguments, and user. The client returns it on approval, and the server validates it before executing. Args cannot be changed between confirmation and execution.

---

## 19. Client-Side Tools

Mobile and web apps often need the AI to trigger UI or device actions (open a screen, set a reminder). These are **declared server-side** and **executed by the SDK**.

* The server emits a `tool_call` event for a client tool; the SDK runs the registered handler and returns the result.
* Client tool results are treated as untrusted input.
* Client tools must not be used for privileged operations; those belong to server-side tools.

---

## 20. Guardrails

Configurable, pluggable guardrails around AI requests.

Pipeline:

```text
Request → Token validation → Authorization → User limits
  → Input guardrails → Prompt construction → Model
  → Tool calls (with tool authorization)
  → Output guardrails → Schema validation → Response
```

Potential guardrails: prompt injection detection, PII detection, content safety, input/output size limits, token limits, structured output validation, custom rules.

### 20.1 Streaming behavior

Output guardrails and streaming conflict, so each AI App picks a mode:

```yaml
guardrails:
  stream_mode: buffered                    # buffered | passthrough_with_post_check
```

* `buffered`: output is checked before delivery (higher latency, safe).
* `passthrough_with_post_check`: tokens stream immediately and checks run afterward. **Already-sent tokens cannot be retracted**; violations are logged and can emit a `retract`/`error` event that the client should honor.

---

## 21. Context: Conversation State and Memory

### 21.1 Conversation state

* Default: **stateless**. The client sends the message history each request.
* Optional: server-side threads with TTL and per-thread deletion.

### 21.2 Memory

Optional per-AI-App memory, scoped to identity rather than globally shared.

Scopes: conversation, user, AI App, semantic.

Namespace example: `project + issuer + subject + ai_app_id`.

A `MemoryProvider` abstraction supports:

* PostgreSQL + pgvector (initial)
* Redis, Qdrant, Pinecone (later)
* **Implementor-hosted providers** via HTTP, so memory can live in the implementor's own store

Memory is opt-in, has TTL, and is deletable by subject (§25.3).

---

## 22. Multimodal and Structured Output

### 22.1 Attachments

* Input types: text, image (v0.1); document/audio later.
* Configurable size and MIME type limits per AI App.
* Uploaded files use temporary storage with TTL and are not retained by default.

### 22.2 Structured output

Each AI App may define a `response_schema` (JSON Schema).

* Output is validated against the schema.
* Optional automatic repair/retry (bounded).
* Schema violations are reported in logs and errors.

---

## 23. Caching

Reduces latency and cost.

Cache types: response cache, tool-result cache, semantic cache.

* Initial: Redis.
* Configurable per AI App and per Tool.
* Cache keys always include project, environment, and the identity scope unless a cache is explicitly marked as shared and non-user-specific.

```yaml
cache:
  enabled: true
  response:
    ttl: 300
    scope: user        # user | shared
```

---

## 24. Usage Tracking

Usage is tracked per: AI App, user, model, provider, request, date, environment.

Metrics: requests, input/output/total tokens, latency, provider cost, cache hits, tool calls, errors, guardrail violations, policy denials, limit rejections.

Usage is visible and filterable in the dashboard, and exportable.

---

## 25. Logging, Observability, and Privacy

### 25.1 Request logs

Each request has a request ID (`req_abc123`). Logs record: request ID, project/environment, AI App, user identity, token validation result, policy result, limit result, prompt version, logical model, actual provider/model, tool calls (names, status, latency), token usage, latency, cost, guardrail results, final status.

**Prompt/response content and tool payloads are not logged by default.**

### 25.2 Telemetry

* OpenTelemetry traces (spans for auth, policy, guardrails, model, each tool call).
* Prometheus metrics.
* `X-Request-Id` propagated to tool calls and provider calls.

### 25.3 Privacy operations

* Configurable **redaction rules** for logs.
* **Retention** configurable per Project.
* **Delete by subject:** `DELETE /v1/admin/subjects/{issuer}/{subject}` purges memory, threads, cached entries, and logs for that user.
* Data export by subject on request.

---

## 26. Dashboard

Main navigation:

```text
Overview
AI Apps · Providers · Models · Prompts · Tools · Guardrails · Policies · Memory
Usage · Logs
Settings (Projects, Environments, Auth, Admin users)
```

### 26.1 Admin authentication

Dashboard and management API authentication is **separate from end-user authentication**: local admin accounts, plus OIDC SSO. Role-based admin access (owner, editor, viewer) and an audit log of configuration changes.

### 26.2 AI App page

Shows status, access policy, prompt version, models and routing, tools, guardrails, context settings, response schema, and limits. Changes are made without touching the client application.

### 26.3 Provider page

Provider, credential status (never the value), available deployments, health, usage.

### 26.4 Tools page

Create and edit custom tools, view schema, test with a sample identity, see version history, error rates, and latency.

### 26.5 Usage page

Filter by date, AI App, user, model, provider, status. Example metrics: requests, tokens, estimated cost, average latency, cache hit rate.

### 26.6 Logs page

Inspect a request end to end: authentication, authorization, limit, prompt version, model, provider, tool calls, tokens, latency, status.

---

## 27. Client SDKs

Thin clients. Each SDK lives in its own repository. Initial targets: Android (Kotlin Multiplatform), iOS (Swift native), Web (TypeScript).

```text
beavend-sdk-kmp   → Android (Maven Central)
beavend-sdk-ios   → iOS, macOS (Swift Package Manager)
beavend-sdk-web   → Browser, Node, Deno, Bun (npm)
```

**SDK responsibilities:** attach the user's token (via a token-provider callback so refreshed tokens are used), HTTP, streaming (SSE), serialization, response parsing, typed errors, retries with backoff, cancellation, client-side tool handlers, confirmation flow.

**The SDK must NOT contain:** provider keys, system prompts, provider selection logic, guardrail config, authorization rules, quota logic, or provider credentials.

**iOS SDK** is a pure Swift implementation (`URLSession` + `AsyncStream`) — no Kotlin or KMP dependency. Public API is idiomatic Swift (`async/await`, `AsyncSequence`), and the surface mirrors the Android SDK.

**Android/KMP SDK** uses Ktor client (OkHttp engine) with kotlinx.serialization and coroutines/Flow. The KMP module structure leaves room to add other targets later without restructuring.

**Web SDK** has zero runtime dependencies and is built with `tsup` for ESM + CJS dual output.

All SDKs are versioned independently of the server and document the minimum compatible server version. The SDK sends `X-Beavend-SDK-Version` on every request to allow server-side version skew detection.

```kotlin
val beavend = Beavend(
    url = "https://ai.example.com",
    project = "my-project",
    tokenProvider = { auth.currentUser.getIdToken() }
)

val assistant = beavend.ai("assistant")

assistant.chat { message("Hello") }
assistant.stream { message("Hello") }
```

Header sent: `Authorization: Bearer <user-jwt>`.

---

## 28. API

### 28.1 Runtime API

```text
POST /v1/apps/{ai_app_id}/chat
POST /v1/apps/{ai_app_id}/stream
POST /v1/apps/{ai_app_id}/cancel
GET  /v1/apps/{ai_app_id}
GET  /v1/apps/{ai_app_id}/memory
POST /v1/apps/{ai_app_id}/memory
POST /v1/apps/{ai_app_id}/confirm
POST /v1/apps/{ai_app_id}/tool-results      # client-side tool results
```

Tools are invoked by the model through the runtime, not called directly by clients. (A direct `/v1/tools/{id}/execute` endpoint, if provided, is admin-only for testing.)

### 28.2 Management API

```text
/v1/admin/projects        /v1/admin/apps        /v1/admin/providers
/v1/admin/models          /v1/admin/prompts     /v1/admin/tools
/v1/admin/guardrails      /v1/admin/policies    /v1/admin/usage
/v1/admin/logs            /v1/admin/subjects
```

Runtime and management APIs are separate, with separate authentication.

### 28.3 Streaming (SSE) events

```text
event: delta          data: {"text": "..."}
event: tool_call      data: {"id": "...", "name": "...", "args": {...}}   # client-side tools
event: confirmation   data: {"tool": "...", "args": {...}, "token": "..."}
event: usage          data: {"input_tokens": 842, "output_tokens": 311}
event: error          data: {"code": "...", "message": "..."}
event: done           data: {"request_id": "req_abc123"}
```

### 28.4 Error taxonomy

| Status | Code | Meaning |
|---|---|---|
| 400 | `invalid_request` | Malformed request or schema violation |
| 401 | `unauthenticated` | Token missing, invalid, or expired |
| 403 | `forbidden` | Policy denied (AI App or tool) |
| 404 | `not_found` | Unknown AI App |
| 422 | `guardrail_blocked` | Input or output guardrail blocked the request |
| 429 | `rate_limited` | Limit exceeded; includes `retry_after` |
| 502 | `provider_error` | Upstream provider failure |
| 503 | `unavailable` | Dependency down (fail-closed) |
| 504 | `tool_timeout` | Tool exceeded its timeout |

---

## 29. Request Lifecycle

```text
Client (User JWT)
  → Beavend API
  → Validate token (signature, exp, iss, aud)
  → Normalize identity
  → Resolve Project / Environment / AI App
  → Evaluate authorization policy
  → Check user limits
  → Run input guardrails
  → Load prompt
  → Load context / memory
  → Resolve tools
  → Select logical model
  → Resolve provider deployment
  → Provider Adapter (LiteLLM) → AI Provider
  → Tool calls: authorize → bind identity → validate → execute → sanitize
  → Output guardrails / schema validation
  → Persist usage and logs
  → Client
```

---

## 30. Projects, Tenancy, and Environments

Beavend supports multiple Projects on one deployment.

```text
Beavend Instance
├── Project A ── environments: dev / staging / prod
│     ├── Auth config
│     ├── AI Apps, Prompts, Policies, Tools, Models
├── Project B
└── Project C
```

* Each Project has isolated configuration, secrets, and auth settings.
* Requests identify their Project via project ID (header or URL) plus origin/bundle allowlists.
* User identity is scoped by `project + issuer + subject`.
* **`environment` is part of the key model from v0.1** (keys, versions, usage) even though multi-environment UX arrives later. This avoids a painful migration.
* Later: Organizations and Teams above Projects.

---

## 31. Configuration as Code

* `beavend export` / `beavend import` for the complete Project configuration in YAML.
* The dashboard and Git stay in sync; drift detection is a later feature.
* Secrets are referenced (`env:`/secret refs), not embedded in exported files.
* Enables promotion of config between environments.

Exported files carry a `beavend_version` field so the importer can detect schema incompatibilities and warn before applying:

```yaml
beavend_version: "0.1"
project: my-project
environment: production
```

Example AI App:

```yaml
app:
  id: assistant
  access_policy: ai-access
  prompt: { version: 3 }
  model: { default: fast }
  routing: { enabled: true }
  tools: [search, get_orders]
  guardrails: [default]
  context:
    conversation: client        # client | server
    memory: { enabled: true, ttl: 30d }
  response_schema: null
  limits: [inference_per_minute, inference_per_day]
  cache: { enabled: true, response: { ttl: 300 } }
```

---

## 32. Deployment

Self-hostable via Docker.

```text
beavend-server
beavend-dashboard
postgres
redis
litellm
(optional) ollama
```

```text
docker compose up
```

Runs locally or on a VPS/cloud host. Helm chart and Kubernetes manifests are later.

---

## 33. Non-Functional Requirements

| Area | Requirement |
|---|---|
| Overhead | Beavend adds under 30 ms p95 (excluding provider and tool time) |
| Scalability | Server is stateless and horizontally scalable; state in Postgres/Redis |
| Availability | Graceful degradation: if Redis is down, use the Postgres counter fallback; if LiteLLM is down, return `503`; limits fail closed by default (configurable) |
| Security | Secrets encrypted at rest, TLS expected, SSRF protections on tool executors (egress allowlists) |
| Streaming | First-token latency overhead under 50 ms p95 in passthrough mode |
| Compatibility | Postgres 15+; documented supported Redis versions |
| Upgradability | Versioned migrations; config schema versioning |
| Licensing | Permissive open-source license (to be decided, see §40) |

---

## 34. Internal Architecture

```text
Beavend
├── API Layer (runtime + management)
├── Identity
│   ├── JWT / OIDC / Firebase / Supabase / Custom
├── Authorization
│   └── Policy Engine (local + external)
├── AI Runtime
│   ├── Prompt Engine
│   ├── Context Engine (conversation, memory)
│   ├── Routing Engine
│   ├── Tool Engine (executors, identity binding, sanitization)
│   ├── Guardrails
│   ├── Memory (MemoryProvider)
│   └── Cache
├── Provider Layer
│   └── ProviderAdapter → LiteLLM (default)
├── Usage & Limits
├── Observability (OTel, Prometheus, logs)
└── Management API + Config-as-code
```

---

## 35. Versioning

Versioned resources: AI App, Prompt, Guardrails, Policies, Tools, Routing.

Each request log records the exact versions used:

```text
assistant  configuration: v12  prompt: v7  routing: v4  guardrails: v3  tools: get_orders@v2
```

---

## 36. Roadmap

### v0.1 — Core platform

```text
Docker deployment
Beavend Server + Dashboard
Projects (environment in key model)
AI Apps
Provider management + ProviderAdapter (LiteLLM)
Generic JWT/OIDC, Firebase, Supabase token validation
Authorization policies (with operators)
Per-user rate limits and quotas (with tiers), counter store (Redis or Postgres)
Server-side prompts
Logical models + basic routing
Streaming (SSE)
Basic structured output
Usage tracking + logs (metadata only)
Admin auth (local + OIDC)
Config export/import
Android SDK (beavend-sdk-kmp), REST API
```

### v0.2 — Tools and safety

```text
Custom tools (HTTP, webhook)
Identity bindings + downstream auth modes
Tool authorization, confirmation, testing
Client-side tools
Guardrails (incl. streaming modes, injection handling)
Prompt versioning
OpenTelemetry / Prometheus
iOS SDK (beavend-sdk-ios, Swift native), Web SDK (beavend-sdk-web)
Provider/model fallback, advanced routing
Multimodal attachments
Privacy: retention, redaction, delete by subject
```

### v0.3 — Context and quality

```text
MCP and SQL-template executors
Conversation threads, memory, MemoryProvider (incl. implementor-hosted)
Redis caching, semantic caching
Evaluation, prompt playground, AI App testing
External policy checks (webhook/OPA)
```

### Future

```text
Agent runtime, workflows, multi-step agents
A/B testing, prompt experiments, evaluation pipelines
Advanced analytics
Organizations and Teams
Full multi-environment UX and promotion
Plugin system, marketplace
```

---

## 37. Relationship With LiteLLM

**LiteLLM** handles provider infrastructure: provider integrations, unified model API, provider credentials, provider routing, fallbacks, model deployments, provider-level usage and cost.

**Beavend** handles application infrastructure: AI Apps, token validation and identity normalization, authorization, per-user limits, prompts, application-level routing, tools and data access, tool authorization, guardrails, memory, caching, client SDKs, dashboard, application-level usage and observability.

Beavend does not duplicate LiteLLM's integration layer, and it accesses LiteLLM through `ProviderAdapter` so it is replaceable.

---

## 38. Product Positioning

> An open-source, self-hosted AI backend for applications.

More specifically: *Firebase AI Logic for self-hosted applications.*

Beavend is not primarily an AI gateway, LLM proxy, or LLM router. Those descriptions focus on provider infrastructure. The core value is:

```text
Application → Beavend → AI capabilities (with your data, your users, your models)
```

**Personas:** the mobile/web developer who wants AI features without building a backend; the small team that needs per-user limits and safety without a platform team; the privacy-sensitive implementor who must self-host and keep data in their own systems.

---

## 39. Example Developer Experience

```text
docker compose up
```

In the dashboard the developer configures:

```text
Authentication → Supabase
Provider       → Google Gemini
Model          → fast = Gemini model
Tool           → get_orders (HTTP, forward_user_token)
AI App         → assistant
Prompt         → assistant-v1
Policy         → authenticated users
Limit          → 30 inference/day
```

Then in the app:

```kotlin
beavend.ai("assistant").chat { message("What's the status of my last order?") }
```

The app needs only the Beavend URL, project ID, AI App ID, and the user's token. It does not need a provider key, LiteLLM key, system prompt, model ID, or routing/guardrail configuration. Changing the model or provider requires no app release.

The AI reads the user's orders live from the implementor's API with the user's own identity, and Beavend stores none of that data.

---

## 40. Open Questions

1. ~~**License:** Apache-2.0, MIT, or AGPL?~~ **Resolved: Apache-2.0.**
2. ~~**Implementation language/runtime** for the server?~~ **Resolved: Go.** (See `techstack.md §3`.)
3. **Default downstream auth mode** for tools when the implementor's API doesn't accept the IdP's JWT?
4. **Should a default `MemoryProvider` ship in v0.1**, or defer entirely to v0.3?
5. **How much of the tool-call loop** (max depth, parallel calls) is configurable vs fixed?
6. **Multi-region / HA reference architecture:** required for v1.0?
7. **Cost estimation** when LiteLLM is bypassed by a custom `ProviderAdapter`.
8. **Trademark, domain, and package-name availability** for "Beavend" (GitHub org, npm, Maven, PyPI, domains) must be verified before public launch.

---

## 41. Success Criteria

Beavend is successful when a developer can:

1. Deploy it with Docker.
2. Connect an existing authentication provider.
3. Create an AI App from the dashboard.
4. Configure an AI provider and a logical model.
5. Create a server-side prompt.
6. Define an authorization policy.
7. Define per-user inference limits, including tiers.
8. Create a custom tool that reads live data from their own API with the user's identity.
9. Call the AI App from Android/KMP/iOS/Web.
10. Stream the response.
11. Apply guardrails and require confirmation for write tools.
12. View usage and inspect logs.
13. Export configuration to Git.
14. Change the provider/model without releasing the client application.

Adoption signals to track: time from `docker compose up` to first successful call (target under 15 minutes), GitHub stars/forks, active self-hosted deployments (opt-in telemetry), and SDK downloads.

The fundamental developer experience:

```text
Bring your existing users.
Bring your data.
Bring your AI provider.
Create an AI App.
Configure its behavior.
Call it from your application.
```

Beavend provides the runtime between the authenticated user, the AI provider, and the implementor's data, without becoming the application's authentication system, its database, or its business logic.
