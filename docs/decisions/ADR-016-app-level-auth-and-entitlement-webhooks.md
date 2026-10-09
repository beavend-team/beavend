# ADR-016: App-Level Authentication, JWT Claims-Based CEL Policies & Scope Alignment

## Status
Accepted (Amended: Entitlement webhooks superseded by JWT Claims & App-Scoped Policies/Identity)

## Context
1. **JWT Claims vs. External Webhooks**:
   Identity providers like Firebase Auth (`customUserClaims`), Supabase Auth (`app_metadata` and `user_metadata`), and custom JWTs (issued by Auth0, Clerk, Cognito, or custom backends) natively support embedding rich custom claims (`plan`, `role`, `tenant_id`, `features`, `email_verified`).
   An initial prototype considered external HTTP entitlement webhooks for forward-authz enrichment. However, external webhooks introduce unnecessary HTTP network hops, external failure modes, additional secret management, and operational complexity. Direct evaluation of JWT `claims` in CEL provides sub-millisecond, zero-dependency, self-contained authorization.

2. **Auth & Policy Granularity (App Scope vs Project Scope)**:
   Individual AI Apps serve different surfaces:
   * Public client-facing chatbots authenticated via Firebase Auth or Supabase.
   * Internal operations copilot authenticated via corporate Google/Okta OIDC.
   * Public search or documentation assistants with anonymous access (`allow_anonymous: true`).
   * Distinct access policies per application surface (e.g. App A requires `claims.plan == 'pro'`, while App B allows all authenticated users).
   Previously, policies and identity were configured at the Project scope or cluttered within general app Settings.

3. **Performance, Latency & Hot-Path Invariants**:
   Beavend enforces a strict non-negotiable invariant: **zero database reads on the hot path**. By relying on validated JWT claims and compiling app-level auth and CEL policies into in-memory `Snapshot` structures, requests evaluate with zero database queries and zero external network hops.

## Decision

1. **App-Level Authentication Configuration (Strictly App-Scoped)**:
   - `core.AIApp` has an `auth` configuration (`AuthConfig`).
   - Project-level identity provider is removed; identity is 100% app-scoped. Each app defines its own provider (`firebase`, `supabase`, `oidc`, `jwt`, `anonymous`), or anonymous access if unconfigured.
   - The runtime validates incoming tokens using the app's specific JWKS and validation parameters.

2. **JWT Claims as the Sole Source of CEL Authorization**:
   - The CEL activation environment exposes:
     - `claims`: All claims present in the validated JWT token (`claims.plan`, `claims.role`, `claims.tenant_id`, etc.).
     - `identity`: Standard identity attributes (`identity.subject`, `identity.issuer`, `identity.audience`, `identity.project`, `identity.environment`).
     - `meta`: Request metadata (`meta.client_ip`).
   - All external entitlement webhook infrastructure and dependencies have been removed.

3. **App-Scoped Dashboard Tabs & Removal of Project-Level Duplication**:
   - "CEL Policies" and "Identity & Auth" are moved from project scope navigation to dedicated first-class tabs in the AI App Workspace (`AppWorkspace`).
   - The App "Settings" tab is kept focused on core configuration (models, tools, rate limits, guardrails, search, memory, cache) with all CEL policy and identity inputs removed.
   - The CEL Policies tab provides an interactive live sandbox and claim-based templates.
   - The Identity & Auth tab provides app-level provider configuration, a live JWT token inspector, and a "Copy Claims to CEL Sandbox" workflow.

## Consequences

### Positive
* **Zero Network Overhead**: Authorization decisions evaluate locally in memory in microseconds with zero external webhook calls.
* **Simplified Operations**: No external webhook URLs, HMAC signature verification, or fail-open/fail-closed edge cases.
* **First-Class App Granularity**: Each AI App owns its authentication provider and CEL access policy cleanly in its own workspace tab.
* **Clean Separation of Concerns**: Settings tab remains clean and uncluttered.

### Negative
* Application developers must ensure necessary user entitlement attributes (e.g., subscription tier, roles) are present in the JWT claims issued by their identity provider.

## Alternatives Considered
* **External Forward-Auth Webhooks**: Evaluated in prototype and rejected due to network latency, failure handling complexity, and operational overhead when JWT claims are already customizable in modern identity providers.
* **Project-Level Global Policies**: Rejected because modern multi-tenant AI backends require granular per-app authorization rules.
