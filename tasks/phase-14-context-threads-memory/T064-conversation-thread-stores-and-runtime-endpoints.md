# T064 — Conversation Thread Stores & Runtime HTTP Endpoints

## Status
Completed

## Objective
Build pluggable conversation thread storage implementations (Redis, PostgreSQL, In-Memory) and expose runtime endpoints for thread lifecycle management.

## Key Changes
1. `internal/memory/threads.go`:
   - `MemoryThreadStore`: in-memory storage with mutex and TTL checks.
   - `RedisThreadStore`: Redis-backed storage with key `th:{project}:{env}:{thread_id}`, native TTLs, and `th:sub:{project}:{env}:{issuer}:{subject}` secondary set index for subject purging.
   - `PostgresThreadStore`: Durable relational storage using `sqlc` queries with timestamped expiration.
2. Runtime HTTP Endpoints:
   - `GET /v1/apps/{ai_app_id}/threads/{thread_id}` -> `HandleGetThread`
   - `DELETE /v1/apps/{ai_app_id}/threads/{thread_id}` -> `HandleDeleteThread`
   - `POST /v1/apps/{ai_app_id}/threads` -> `HandleCreateThread`
3. Turn Appending in Request Lifecycles:
   - `HandleChat` and `HandleStream` append newly exchanged turns (user message + assistant completion) to the active thread upon completion.
   - Response bodies and SSE events return `thread_id`.
