# T066 — Config-as-Code Declarative Schemas and Serializers

## Status
Completed

## Objective
Establish pure declarative domain representations and YAML serializers for Beavend project configurations (`beavend_version: "0.1"`) under `internal/configcode` with strict validation, zero loss of metadata, and safe secret reference formatting.

## Key Changes
1. `internal/configcode/schema.go`:
   - `ProjectDocument`: `BeavendVersion`, `Project`, `Environment`, `Auth`, `AIApps`, `Prompts`, `Models`, `Tools`, `Policies`, `Limits`.
   - `AppConfig`: ID, Name, Description, AccessPolicy, PromptRef, Routing, ToolIDs, Guardrails, Memory, Cache, ResponseSchema, LimitIDs.
   - `PromptConfig`: ID, Name, Description, SystemPrompt, Template, Variables, Version.
   - `ModelConfig`: ID, Name, Provider, ProviderConfig, SecretRefs, Aliases.
   - `ToolConfig`: ID, Name, Description, Executor, Schema, DownstreamAuth, IdentityBindings, Confirmation, Cache.
   - `PolicyConfig`: ID, Name, Description, Rules, Effect.
   - `LimitConfig`: ID, Name, Tiers, WindowSeconds, MaxRequests, MaxTokens.
2. YAML serialization / deserialization helpers with deterministic ordering and field tags.
3. Secret masking utilities (`[SECRET_MASKED]`, `${ENV_VAR}`, `env:VAR`).
