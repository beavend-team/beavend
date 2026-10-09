# ADR-003: External Identity Validation Without Token Revocation Tracking

## Status
Accepted

## Context
Beavend serves as the AI backend for existing applications. These applications already employ established identity providers (Firebase Auth, Supabase Auth, Auth0, Clerk, Keycloak, or custom OIDC servers).

We evaluated whether Beavend should implement a token revocation and session management subsystem (e.g. distributed token denylists, active session tables, or token introspection checks).

## Decision
We decided that **Beavend validates and forwards tokens, but explicitly does not track or manage token revocation or session lifecycle**.

Specifically:
1. Beavend validates token signatures locally using cached JWKS retrieved from the implementor's identity provider.
2. It verifies token expiration (`exp`), not-before (`nbf`), issuer (`iss`), and audience (`aud`).
3. If valid, the token is accepted. Beavend maintains no denylist or token blacklist in Redis or PostgreSQL.
4. Token lifecycle, revocation, and session termination remain strictly the responsibility of the implementor's identity provider.
5. Implementors requiring near-instant access revocation must issue short-lived JWTs (e.g., 5 to 15 minutes) or utilize `forward_user_token` mode, enabling downstream application APIs to reject revoked tokens at data access time.

## Consequences
### Positive
* **Zero Network Latency:** Token validation is 100% local CPU work using in-memory cached JWKS.
* **Architecture Purity:** Prevents Beavend from duplicating identity provider functionality or maintaining complex distributed synchronization with external IdPs.
* **Massive Concurrency:** Eliminates hot shared state or database lookups for session validation.

### Negative
* A revoked token remains valid for AI requests until its `exp` timestamp expires, unless downstream tools reject it.

## Alternatives Considered
* **OAuth 2.0 Token Introspection (RFC 7662):** Rejected because calling an external introspection endpoint on every AI request would add unacceptable latency and create an external point of failure.
* **Internal Token Blacklist / Denylist in Redis:** Rejected because it would require IdP webhooks, complex cross-region sync, and violates the principle that Beavend does not own user session state.
