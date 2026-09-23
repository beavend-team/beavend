# T010 — JWT Validation and Cached JWKS Verifier

## Goal
Implement local, zero-network-on-hot-path JWT validation using `lestrrat-go/jwx` with in-memory cached JWKS in `internal/identity`.

## Depends On
- T001
- T002

## Scope
* Implement `JWKSManager` to fetch and cache JWKS keysets from remote OIDC providers (e.g. Firebase, Supabase, Auth0).
* Implement background automatic refresh on unknown `kid` (key rotation handling).
* Implement token parser validating:
  * Signature against cached JWKS.
  * Algorithm enforcement (whitelist `RS256`, `ES256`, `EdDSA`; reject `none` and symmetric algs).
  * Expiration (`exp`) and not-before (`nbf`) with 60-second clock skew tolerance.
  * Issuer (`iss`) and audience (`aud`) matching project config.

## Requirements
- Token validation must not perform network calls on known keys.
- Thread-safe JWKS cache with configurable TTL.

## Constraints
- Do not implement token revocation lists or database session checks (per ADR-003).

## Acceptance Criteria
- [ ] Valid tokens pass signature and claims verification.
- [ ] Expired tokens, tokens with wrong issuer/audience, or tokens with `alg: "none"` fail with specific typed errors.
- [ ] Unknown `kid` triggers controlled single-flight JWKS refresh.

## Verification
```bash
go test -race -v ./internal/identity/...
```
