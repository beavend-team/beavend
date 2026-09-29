# T053: Tool Result Caching in Bounded Agent Loop

## Status
Completed

## Context & Objectives
Tools frequently fetch idempotent or semi-static read data (e.g. product catalog lookups, user profile fetches, currency rates). Executing these network calls repeatedly within or across requests degrades latency and strains downstream implementor services. When a tool specifies `cache.enabled = true`, its execution results must be cached.

Key requirements:
1. Integrate `core.CacheStore` into `agent.LoopConfig` and `agent.Loop`.
2. For any tool execution where `toolDef.Cache.Enabled = true`:
   - Compute a stable hash of the tool's bound/validated arguments.
   - Build cache key: `tc:{project}:{env}:{user}:{tool}:{argshash}`.
   - If present in cache, return the cached `core.ToolResult` directly without invoking the underlying executor.
   - If not in cache, execute the tool. On successful execution (non-error, not suspended), store the sanitized result in `CacheStore` with TTL `toolDef.Cache.TTL` (default 300s).
3. Works consistently in both unary (`runUnaryTurn`) and streaming (`runStreamingTurn`) agent loop execution.
4. Uphold security: tool caching must respect user isolation (never share user-specific tool results across different users).

## Deliverables
* Update `internal/runtime/agent/loop.go` to support tool caching via `core.CacheStore`.
* Update `internal/runtime/agent/loop_test.go` with tests verifying tool cache hit, miss, TTL, and user isolation.
