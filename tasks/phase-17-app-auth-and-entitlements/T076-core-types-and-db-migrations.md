# T076 - Core Types, Database Schema & SQLC Queries for App Auth

* **Status:** Completed

## Goal
Extend core domain models (`core.AIApp`, `core.ReqState`) to support per-app authentication overrides, add the required PostgreSQL migration for `auth_config`, update `sqlc` queries, and remove webhook entitlement fields in favor of direct JWT claims.

## Scope
1. **Domain Models (`internal/core/`):**
   - In `internal/core/app.go`:
     - Add `AuthConfig *AuthConfig` to `AIApp`.
     - Remove `EntitlementWebhook` and `EntitlementWebhookConfig`.
     - `ReqState` carries `Identity` with `Claims` from the validated token.
2. **Database Migration (`internal/store/migrations/00007_app_auth_and_entitlements.sql`):**
   - Add column to `ai_apps`:
     - `auth_config JSONB DEFAULT NULL`
3. **SQLC Queries (`internal/store/queries/ai_apps.sql` & `snapshot.sql`):**
   - Update `CreateAIApp`, `UpdateAIAppActiveVersion`, `GetAIApp`, `ListAIApps` to include `auth_config`.
   - Update `snapshot.sql` queries to select `auth_config`.
   - Run `sqlc generate`.
4. **Unit Tests:**
   - Verify serialization/deserialization and defaults in core types test.

## Acceptance Criteria
- [x] Core types updated with proper JSON/YAML tags.
- [x] Migration applies cleanly without affecting existing AI Apps.
- [x] SQLC generates updated Go structs and query functions without entitlement webhook fields.
