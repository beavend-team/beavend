# T062 — PostgreSQL + pgvector Schema Migration, sqlc Queries & PGVectorMemoryProvider

## Status
Completed

## Objective
Implement durable relational storage and vector similarity indexing in PostgreSQL with the `vector` extension, generate type-safe sqlc queries, and build the production `PGVectorMemoryProvider`.

## Key Changes
1. Database Migration `internal/store/migrations/00005_context_and_memory.sql`:
   - Enables `vector` extension (`CREATE EXTENSION IF NOT EXISTS vector;`).
   - Creates `conversation_threads` table with TTL and subject indexes.
   - Creates `semantic_memories` table with vector embeddings column and identity composite indexes.
   - Adds `memory_config` column to `ai_app_versions`.
2. sqlc Queries:
   - `internal/store/queries/threads.sql`: `CreateConversationThread`, `GetConversationThread`, `UpdateConversationThreadMessages`, `DeleteConversationThread`, `DeleteConversationThreadsBySubject`, `DeleteExpiredConversationThreads`.
   - `internal/store/queries/memory.sql`: `CreateSemanticMemory`, `RecallSemanticMemories`, `SearchSemanticMemories` (using cosine distance `<=>`), `DeleteSemanticMemoriesBySubject`, `DeleteExpiredSemanticMemories`.
3. `PGVectorMemoryProvider` in `internal/memory/postgres.go`:
   - Implements `core.MemoryProvider` over `*db.Queries`.
   - Converts `[]float32` embeddings to/from `pgvector.Vector`.
   - Applies threshold and limit filters on vector similarity queries.
