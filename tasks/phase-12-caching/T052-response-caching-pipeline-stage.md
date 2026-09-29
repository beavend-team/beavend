# T052: Response Caching Pipeline Stage

## Status
Completed

## Context & Objectives
When an AI App has caching enabled (`AIApp.Cache.Enabled = true`), repeated identical requests should be served directly from the cache to avoid unnecessary upstream LLM calls, saving time and tokens.

Key requirements:
1. Implement `CacheStage` in `internal/runtime/stages/cache.go` satisfying `core.Stage`.
2. Compute a stable cryptographic hash (SHA-256) of the normalized request:
   - Ordered messages (role, content, tool calls)
   - Logical model & temperature/parameters
   - Response schema and prompt version if applicable
3. Resolve cache key based on `AIApp.Cache.Scope`:
   - `shared`: Key is `rc:{project}:{env}:shared:{hash}`
   - `user`: Key is `rc:{project}:{env}:user:{subject}:{hash}` (falls back to caller subject or anonymous)
4. On Cache Hit:
   - Unmarshal cached `core.ChatResponse`.
   - Update `state.ChatResponse` with a fresh request ID and timestamp while preserving cached content and finish reason.
   - Set `state.Metadata["cache_hit"] = true`.
   - In streaming mode (`state.IsStreaming`), stream the cached response text to `state.StreamChan` as SSE deltas followed by done event so the client streaming contract is preserved.
   - Mark the pipeline execution so downstream stages (prompt, routing, execution) know a response has already been produced.
5. On Cache Miss:
   - Allow execution to proceed down the pipeline.
   - Record cache miss in `state.Metadata["cache_hit"] = false`.

## Deliverables
* `internal/runtime/stages/cache.go`: `CacheStage` implementation.
* `internal/runtime/stages/cache_test.go`: Unit tests covering cache hits, cache misses, shared vs user scopes, and streaming replay.
