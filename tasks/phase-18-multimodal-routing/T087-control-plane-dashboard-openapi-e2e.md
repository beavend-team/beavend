# T087 - Control Plane, Dashboard, OpenAPI & E2E

* **Status:** Completed

## Goal
Expose configuration and prove the whole flow.

## Scope
1. `api/openapi.yaml`: `output_modalities`, media parts, job endpoints, `model_routes` config.
2. Control handlers for `model_routes` and model capabilities; audit logging; config-as-code (ADR-013) export/import.
3. Dashboard: "Routing" tab in AppWorkspace (rule builder with model pickers filtered by capability); capability fields in model editor.
4. E2E tests: routing by image/audio input and image output, cache isolation, thread continuity across models.
5. Update `docs/architecture.md`, `docs/api.md`; finalize ADR-017 status.

## Acceptance Criteria
- [ ] `golangci-lint`, `go test -race ./internal/...`, integration suite pass.
