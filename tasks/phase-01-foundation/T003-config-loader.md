# T003 — Application Configuration Loader

## Goal
Implement environment variable and YAML-based configuration loading for server instances in `internal/config`.

## Depends On
- T001
- T002

## Scope
* Define `Config` struct encompassing HTTP, Database, Redis, LiteLLM, and Security settings.
* Implement loader reading from environment variables with default values.
* Implement configuration validation rejecting invalid port numbers, missing master encryption keys, or malformed database connection strings.

## Requirements
- Support flags: `CORTEXBASE_ROLE` (`all`, `runtime`, `control`).
- Require `CORTEXBASE_MASTER_KEY` (32 bytes hex) when running in production environment.
- Provide sensible defaults for local development.

## Constraints
- Do not log secret values or connection strings containing passwords.

## Acceptance Criteria
- [x] Loads valid configuration from environment.
- [x] Fails immediately on invalid configuration values with informative error messages.
- [x] Unit tests cover default fallback, env override, and validation failures.

## Verification
```bash
go test -v ./internal/config/...
```
