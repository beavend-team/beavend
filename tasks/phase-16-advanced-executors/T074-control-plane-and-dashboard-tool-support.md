# T074 — Control Plane & Dashboard Advanced Tool Support

## Status
Completed

## Objective
Update control plane schema validation, management endpoints, and dashboard UI to allow administrators to configure, validate, and test MCP and SQL Template tools.

## Requirements & Scope
1. Control Plane & OpenAPI:
   - Validate executor types `"mcp"` and `"sql_template"` in tool creation/update payloads (`POST/PUT /v1/admin/tools`).
   - Validate SQL template syntax (ensure non-empty template and named parameters matching input schema).
   - Ensure `POST /v1/admin/tools/{id}/test` supports MCP and SQL template execution.
2. Dashboard UI:
   - Update Tool editor form in `dashboard/`:
     - Add executor selector: HTTP, Webhook, MCP, SQL Template.
     - For MCP: URL, endpoint, headers, downstream auth.
     - For SQL Template: DSN/connection string, SQL query editor with parameter hints.
   - Test Tool modal supports live testing for MCP and SQL tools.
3. Verification:
   - Dashboard build compiles without errors.
