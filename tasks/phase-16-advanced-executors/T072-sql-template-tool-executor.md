# T072 - Parameterized SQL Template Tool Executor

## Status
Completed

## Objective
Implement a parameterized read-only SQL template tool executor that connects to implementor databases (PostgreSQL, SQLite, MySQL) using pooled connections, binds model arguments and identity claims securely, and returns structured JSON rows without arbitrary SQL injection risks.

## Requirements & Scope
1. Implement `tools.SQLTemplateExecutor` satisfying `core.Executor`:
   - Enforces read-only statements: Rejects mutating statements (`INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`, `TRUNCATE`, `GRANT`, `REVOKE`, etc.) at parse/validation time.
   - Parses named parameters (e.g. `:user_id`, `:limit`, `:offset`, `:status`) from `template`.
   - Converts named parameters into dialect-safe positional placeholders (`$1`, `$2` for PostgreSQL; `?` for SQLite/MySQL).
   - Validates that all supplied arguments map to expected types and prevents missing parameter leaks.
   - Executes query using connection pooling with query timeout (`timeout_ms`).
   - Maps database rows and column types to JSON objects (`[]map[string]any`).
   - Marshals row results to JSON string and returns `core.ToolResult`.
2. Connection management:
   - Provide a connection registry or cache for DSNs to reuse pooled `*sql.DB` connections efficiently.
3. Unit tests covering:
   - Read-only enforcement (blocking dangerous keywords and multi-statement queries).
   - Named parameter extraction and substitution with dialect formatting.
   - Query execution against an in-memory SQLite database and PostgreSQL test harness.
   - Row serialization and error reporting.
