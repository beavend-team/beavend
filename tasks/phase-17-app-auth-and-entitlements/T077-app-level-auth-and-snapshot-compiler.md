# T077 - Snapshot Compiler & App-Level Authentication Resolution

* **Status:** Completed

## Goal
Update the Snapshot Compiler to compile `app.AuthConfig` into in-memory `Snapshot.Apps`, and update the runtime authentication ingress to resolve `AuthConfig` strictly per AI App (project-level identity provider removed).

## Scope
1. **Snapshot Compiler (`internal/snapshot/compiler.go`):**
   - Unmarshal `auth_config` from DB records when compiling `Snapshot.Apps`.
   - Pre-resolve JWKS if custom app-level provider is specified.
2. **Auth Ingress & Middleware (`internal/identity/` & `internal/runtime/handler.go`):**
   - Update auth middleware / runtime request handler:
     - Determine target `AIApp` from route `/v1/apps/{app_id}/*`.
     - Resolve `AuthConfig` directly from `app.AuthConfig`.
     - If provider is `anonymous`, allow access without token, populating anonymous `core.Identity`.
     - If provider requires validation, validate using the app's effective provider validator.
3. **Unit Tests:**
   - Test snapshot compiler populating app auth configs.
   - Test app-level auth resolution (app providers vs anonymous).

## Acceptance Criteria
- [x] Snapshot compiler compiles app auth configs with zero allocations on the hot path.
- [x] Requests to `/v1/apps/{id}/*` correctly authenticate against the app's auth config.
- [x] Identity & auth is strictly app-scoped without inheritance or project-level fallback.
