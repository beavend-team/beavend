# T079 - JWT Claims & AuthZ Pipeline Stage Integration

* **Status:** Completed

## Goal
Integrate app-level CEL access policies into the runtime `AuthZStage`, evaluating policies directly against token `claims`, `identity`, and request `meta` without external webhook calls.

## Scope
1. **CEL Compiler & Evaluator (`internal/policy/`):**
   - Retain standard variables: `claims` (dynamic map of JWT claims), `identity` (subject, issuer, audience, project, environment), and `meta` (client_ip).
   - Removed `entitlements` variable and context parameters.
2. **AuthZ Stage (`internal/runtime/stages/authz.go`):**
   - Check if `state.AIApp.AccessPolicy != ""`:
     - Evaluate CEL policy passing `state.Identity.Claims`, `state.Identity`, and `state.Metadata`.
     - Deny access (`403 Forbidden`) if CEL expression evaluates to false.
     - Return clear compilation/evaluation diagnostics on failure.
3. **Unit Tests:**
   - Table-driven tests in `internal/runtime/stages/authz_test.go`:
     - Verification of plan claims (`claims.plan in ['pro', 'team']`).
     - Verification of email verification (`claims.email_verified == true`).
     - Verification of tenant ID and role attributes.
     - Fallback when no policy is configured.

## Acceptance Criteria
- [x] CEL compiles and evaluates expressions referencing `claims.*`.
- [x] Pipeline stage rejects unauthorized requests before routing, prompting, or LLM execution.
- [x] Table-driven tests achieve >90% coverage for authz stage with token claims.

