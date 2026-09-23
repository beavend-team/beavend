# T011 — Identity Normalization and Claims Extraction

## Goal
Transform validated provider-specific JWT tokens into the normalized `core.Identity` structure in `internal/identity`.

## Depends On
- T002
- T010

## Scope
* Implement identity normalizer interface and adapters for:
  * Generic OIDC
  * Firebase Auth (extracting Firebase-specific claims like `user_id`, `sign_in_provider`)
  * Supabase Auth (extracting `role`, `app_metadata`, `user_metadata`)
* Construct the canonical `core.Identity`:
  ```go
  type Identity struct {
      Project     string
      Environment string
      Issuer      string
      Subject     string
      Audience    string
      Claims      map[string]any
  }
  ```
* Compute unique stable user key: `project + ":" + issuer + ":" + subject`.

## Requirements
- Flatten nested claims into dot-notation accessible paths for CEL evaluation (e.g. `user_metadata.plan` → `claims["user_metadata.plan"]`).

## Constraints
- Claims map must be immutable once created.

## Acceptance Criteria
- [ ] Table-driven tests verify correct normalization across Firebase, Supabase, and generic OIDC sample tokens.
- [ ] Dot-notation claim extraction verified.

## Verification
```bash
go test -v ./internal/identity/normalizer_test.go
```
