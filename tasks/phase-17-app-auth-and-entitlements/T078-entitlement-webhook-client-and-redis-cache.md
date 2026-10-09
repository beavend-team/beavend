# T078 - JWT Claims as CEL Authorization Model & Webhook Removal
 
* **Status:** Completed

## Goal
Standardize on validated JWT token claims as the primary, self-contained mechanism for CEL policy evaluation, and remove external entitlement webhooks and their associated Redis caching to eliminate unnecessary network latency, external dependencies, and operational overhead.

## Scope
1. **Rationale & Architecture:**
   - Identity providers (Firebase custom claims, Supabase `app_metadata`/`user_metadata`, OIDC/JWT providers) natively support embedding claims like `plan`, `role`, `tenant_id`, and `features` directly in tokens.
   - Using token claims preserves Beavend's zero-DB and zero-external-network-hop hot-path guarantees.
2. **Removal of Webhook Infrastructure:**
   - Removed `internal/policy/webhook.go` and `internal/policy/webhook_test.go`.
   - Removed `EntitlementWebhook` field from `core.AIApp` and `ReqState.Entitlements`.
   - Removed external webhook resolution from `internal/runtime/stages/authz.go`.
   - Kept CEL policy evaluation purely driven by `claims`, `identity`, and `meta`.
3. **Unit Tests:**
   - Added table-driven tests verifying CEL policies evaluated against diverse JWT claims patterns (`TestAuthZStage_JWTClaimsCELPolicies`).

## Acceptance Criteria
- [x] Webhook client and external entitlement enrichment completely removed.
- [x] CEL policies evaluate directly against JWT token claims.
- [x] In-memory hot path evaluates in microseconds without network I/O.

