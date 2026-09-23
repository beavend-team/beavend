# Cortexbase Security Architecture

> **Status:** Confirmed Architecture (v0.1) & Future Considerations

---

## 1. Security Context & Trust Boundaries

Cortexbase sits in a security-critical intermediary position:

```text
End User (Mobile / Web SDK)
     │  1. Ingress Boundary: Bearer JWT (Untrusted Client)
     ▼
┌────────────────────────────────────────────────────────┐
│ Cortexbase Runtime Plane                               │
│  - Token validation & Identity normalization           │
│  - CEL Policy Evaluation                               │
│  - Per-user rate limits / Quotas                       │
│  - Input / Output Guardrails                           │
│  - Identity Binding Injection                          │
│  - Untrusted Tool Result Sanitization                  │
└───────┬────────────────────────┬───────────────────────┘
        │                        │
        │ 2. Tool Boundary       │ 3. Provider Boundary
        │ (Forwarded JWT / HMAC) │ (Server-side API keys)
        ▼                        ▼
Implementor APIs / DBs / MCP   LiteLLM / AI Providers
```

### Trust Boundaries:
1. **Boundary 1 (Client ↔ Runtime):** High threat level. Ingress requests from internet-exposed mobile and web apps.
2. **Boundary 2 (Runtime ↔ Implementor APIs):** Medium threat level. Runtime accesses implementor internal services on behalf of the authenticated user.
3. **Boundary 3 (Runtime ↔ AI Providers / LiteLLM):** Upstream model execution. Provider secrets must never leak to Boundary 1.
4. **Boundary 4 (Admin Dashboard ↔ Control Plane):** High privilege boundary for system configuration and secret management.

---

## 2. Authentication & Identity Boundaries

### 2.1 Runtime JWT Validation (Pure Local CPU)
Cortexbase **validates and forwards** tokens; it does not issue tokens.
On every runtime request:
* **Signature Verification:** Verified using cached JWKS retrieved from the implementor's identity provider (e.g. Firebase, Supabase, generic OIDC). JWKS is cached in memory with automatic asynchronous refresh upon unknown `kid` (key rotation).
* **Claims Checked:**
  * `exp`: Must be in the future.
  * `nbf`: Must be in the past (tolerating up to 60s clock skew).
  * `iss`: Must exactly match the configured project issuer.
  * `aud`: Must contain the configured client audience.
* **Algorithm Whitelist:** Only approved asymmetric algorithms (`RS256`, `ES256`, `EdDSA`) are accepted. The algorithm `none` and symmetric signing (`HS256` unless explicitly configured with a pre-shared secret) are strictly rejected to prevent algorithm confusion attacks.

### 2.2 Token Revocation Explicitly Out of Scope
In accordance with PRD §7.4:
* Cortexbase maintains **no session store, denylist, or token revocation list**.
* Token lifecycle, expiry, and revocation are the sole responsibility of the implementor's identity provider.
* Implementors requiring instant cutoff must configure short token lifetimes at their IdP or use the `forward_user_token` tool downstream auth mode, allowing their downstream APIs to reject revoked tokens directly.

### 2.3 Normalized Identity
Once validated, the token is converted into an immutable `Identity` struct:
```text
Identity {
    Project:     "proj_ecommerce",
    Environment: "production",
    Issuer:      "https://auth.example.com",
    Subject:     "usr_998273",
    Audience:    "my-mobile-app",
    Claims:      map[string]any{ "role": "member", "plan": "pro" }
}
```
The internal stable user key is composite: `project + ":" + issuer + ":" + subject`.

---

## 3. Authorization & Tenant Isolation

### 3.1 Policy Evaluation via CEL
* Access to an AI App is controlled by authorization policies compiled into Google CEL (Common Expression Language) programs at snapshot compilation time.
* Policies evaluate solely against the verified `Identity` and request metadata:
  ```cel
  claims.ai_access == true && claims.plan in ['pro', 'enterprise']
  ```
* Sandboxed execution: CEL expressions cannot perform I/O, execute loops, or consume unbounded memory.

### 3.2 Tenant & Environment Isolation
* Every database record in PostgreSQL carries `project_id` and `environment`.
* PostgreSQL Row-Level Security (RLS) is applied as defense-in-depth on all tenant tables.
* Redis cache keys and limit counters are strictly prefixed:
  `lim:{project}:{env}:{issuer_hash}:{subject_hash}:...`
* Automated CI integration tests explicitly assert that requests from Project A cannot access or mutate resources in Project B.

---

## 4. Tool Execution & Data Security

### 4.1 Confused-Deputy Defense (Identity Bindings)
When an LLM requests a tool (e.g. `get_orders(limit=5)`), the user ID parameter is **never accepted from the LLM output**:
1. Tool definition specifies `identity_bindings`:
   ```yaml
   identity_bindings:
     user_id: identity.subject
   ```
2. The runtime forces the parameter value from the validated `Identity.Subject`. Any value supplied by the LLM is discarded.
3. This guarantees an end user cannot manipulate prompt context to trick the LLM into accessing another user's private data.

### 4.2 Tool Authorization
Access to an AI App does not grant access to its tools. Each tool possesses its own authorization policy evaluated dynamically before invocation.

### 4.3 SSRF Containment on HTTP Tool Executors
To prevent Server-Side Request Forgery against internal networks, cloud metadata services, and Kubernetes infrastructure:
1. **Network Egress Denylist:** The HTTP executor uses a custom Go `net.Dialer` with a control hook that resolves DNS and verifies the target IP:
   * Loopback (`127.0.0.0/8`, `::1`)
   * RFC 1918 Private ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`)
   * RFC 3927 Link-local (`169.254.0.0/16`)
   * Cloud Metadata Services (`169.254.169.254`, `metadata.google.internal`)
2. **DNS Rebinding Protection:** The connection is dialed directly to the verified resolved IP; the hostname is passed solely in the HTTP `Host` header.
3. **Per-Project Egress Allowlist:** Optional strict allowlist of allowed domain suffixes or CIDRs.

### 4.4 Untrusted Data Wrapping & Indirect Prompt Injection
* All data returned by tools, databases, or client-side tools is classified as **untrusted external content**.
* Tool outputs are sanitized:
  * Truncated to `max_bytes` (default 20KB).
  * Redacted based on configured regex patterns (credit cards, SSNs).
  * Wrapped in structured delimiters before presentation to the LLM to prevent prompt injection hijacking the agent's instructions.

---

## 5. Secrets Management & Envelope Encryption

### 5.1 Provider Keys & Downstream Secrets
* AI provider credentials (OpenAI, Anthropic, Gemini API keys) and downstream tool secrets (basic auth, private bearer tokens) are stored in PostgreSQL.
* All secrets are encrypted at rest using **AES-256-GCM Envelope Encryption**:
  * Data Encryption Keys (DEKs) are generated per record.
  * DEKs are encrypted using a Key Encryption Key (KEK) managed by a pluggable `KeyProvider` (Environment variable for local dev; AWS KMS, GCP KMS, or HashiCorp Vault in production).
* Plaintext secrets **never leave the server network**. They are never returned over any REST endpoint or printed in logs.

---

## 6. Denial of Service, Abuse Prevention & Limits

### 6.1 Multi-Tier Limits
* Per-user inference limits and token quotas are enforced atomically via Redis Lua scripts before invoking upstream providers.
* Failure mode is configurable; defaults to **fail-closed** (`503 unavailable`) if Redis is unreachable.

### 6.2 Stream & Connection Discipline
* HTTP ingress enforces strict read and header timeouts.
* Server-Sent Events (SSE) connections run heartbeats to detect disconnected clients.
* When client disconnects, `ctx.Done()` aborts upstream LLM generation immediately, conserving provider tokens and server resources.
* Body payload limit: Ingress limits request bodies to 2MB (or configured attachment maximum).

---

## 7. Logging, Observability & Privacy

### 7.1 Metadata-Only Logging by Default
* Request logs capture: `request_id`, `project_id`, `environment`, `user_id_hash`, `ai_app_id`, `model`, latency, token counts, and error codes.
* **Excluded by default:** Prompt text, LLM completion text, and tool payload inputs/outputs are **never** logged unless explicitly enabled on an AI App for debugging.
* When payload logging is enabled, strict PII redaction rules are executed before disk write.

### 7.2 Subject Privacy Purge (GDPR / CCPA)
* Cortexbase provides an administrative endpoint:
  `DELETE /v1/admin/subjects/{issuer}/{subject}`
* This triggers an atomic purge of:
  * Active conversation threads and messages.
  * Semantic vector memories.
  * Cached responses and tool results.
  * User-scoped limit counters.

---

## 8. Summary: Confirmed vs. Future Decisions

| Capability | Status in v0.1 | Future Consideration |
|---|---|---|
| JWT Signature & Claims Validation | Confirmed | mTLS client certificates |
| CEL Policy Enforcement | Confirmed | External OPA / Webhook policy checks (v0.3) |
| SSRF-safe HTTP Tool Executor | Confirmed | Outbound forward proxy sidecar |
| AES-256-GCM Envelope Encryption | Confirmed | Automated DEK re-wrapping & key rotation |
| Metadata-Only Request Logging | Confirmed | Differential privacy log aggregations |
| Delete-by-Subject Endpoint | Confirmed | Automated retention partition pruning |
