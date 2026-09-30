# T073 — Executor Registry & Agent Loop Integration

## Status
Completed

## Objective
Wire the new MCP and SQL template executors into the `DefaultExecutorRegistry` in `internal/runtime/agent/loop.go` and ensure seamless execution during both unary and streaming agent loop iterations.

## Requirements & Scope
1. Update `DefaultExecutorRegistry`:
   - Support `mcp`: Construct `tools.NewMCPExecutor` with safe HTTP client and authenticator.
   - Support `sql_template`: Construct `tools.NewSQLTemplateExecutor` with connection pool registry.
2. In-memory connection lifecycle:
   - Graceful cleanup and connection caching for database handles.
3. Server wiring in `cmd/beavend` / `internal/runtime`:
   - Ensure runtime options pass connection registry to loop config.
4. Loop integration tests:
   - Multi-turn execution where LLM calls an MCP tool or SQL template tool.
   - Result caching and sanitization verification.
