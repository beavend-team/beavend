# T081 - Core Types, Route Config & Migration

* **Status:** Completed

## Goal
Introduce user-defined model routing rules and modality fields (ADR-017).

## Scope
1. `internal/core/app.go`: add `ModelRoutes{Default string; Rules []ModelRouteRule}`; `ModelRouteRule{When RouteCondition; Use string}`; `RouteCondition{InputHas, Output []string; Metadata map[string]string; Expr string}`.
2. `internal/core/model.go`: add `InputModalities`, `OutputModalities` to `ModelRouting`.
3. `ChatRequest`: add `OutputModalities []string`; `Attachment` gains audio/video types and content digest.
4. Migration `00009_model_routes.sql`: `ai_apps.model_routes JSONB`, `model_routings.input_modalities/output_modalities TEXT[]`. Backfill `default` from `model_routing_id`.
5. Update sqlc queries (`ai_apps.sql`, `models.sql`, `snapshot.sql`), run `sqlc generate`.

## Acceptance Criteria
- [x] Migration applies cleanly and backfills existing apps.
- [x] Serialization/default tests pass.
