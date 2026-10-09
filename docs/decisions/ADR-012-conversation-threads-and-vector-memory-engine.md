# ADR-012: Conversation Threads and Vector Memory Engine

## Status
Accepted

## Context
Client applications (mobile, web, and chat interfaces) require conversational continuity and persistent user context across interaction sessions. In standard LLM backends, clients are forced to maintain and transmit the entire message history on every single request turn, which rapidly exhausts client network bandwidth, complicates mobile state management, and risks cross-device desynchronization. Furthermore, AI applications frequently require personalized long-term knowledge retention (e.g., user preferences, saved notes, profile attributes) without violating the core invariant that Beavend stores zero implementor business data.

In accordance with PRD §21.1 (Conversation State), PRD §21.2 (Memory), PRD §25.3 (Privacy Operations), and Architecture §7.3, Beavend requires:
1. Server-side conversation sessions (Threads) with configurable TTL, message history appending, and explicit deletion.
2. User-scoped vector semantic memory with pluggable providers (PostgreSQL + pgvector, implementor-hosted HTTP APIs, and local in-memory engines).
3. Automatic GDPR/CCPA privacy deletion hooks when an administrative subject purge is executed.

## Decision
1. **Core Domain Interfaces & Models:**
   - Define `core.MemoryProvider` with four primary operations:
     - `Store(ctx context.Context, record MemoryRecord) error`
     - `Recall(ctx context.Context, filter MemoryFilter) ([]MemoryRecord, error)`
     - `Search(ctx context.Context, query MemorySearchQuery) ([]MemorySearchResult, error)`
     - `DeleteBySubject(ctx context.Context, projectID, issuer, subject string) error`
   - Define `core.ThreadStore` for session management:
     - `GetThread(ctx context.Context, projectID, env, threadID string) (*Thread, error)`
     - `CreateThread(ctx context.Context, thread *Thread) error`
     - `AppendMessages(ctx context.Context, projectID, env, threadID string, messages []ChatMessage, ttl time.Duration) error`
     - `DeleteThread(ctx context.Context, projectID, env, threadID string) error`
     - `DeleteThreadsBySubject(ctx context.Context, projectID, env, issuer, subject string) error`

2. **Pluggable Vector Memory Providers:**
   - **PostgreSQL + pgvector (`PGVectorMemoryProvider`):** Durable, user-scoped semantic memory table (`semantic_memories`) indexed by `(project_id, environment, issuer, subject, ai_app_id)` using pgvector cosine similarity distance (`<=>`).
   - **Implementor-Hosted HTTP Provider (`HTTPMemoryProvider`):** Enables implementors to store and vectorize memory in their own external infrastructure over HTTP APIs (`/store`, `/recall`, `/search`, `/delete-by-subject`).
   - **Local In-Memory Provider (`LocalMemoryProvider`):** Pure Go cosine similarity engine with RWMutex concurrency control for zero-dependency local testing and ephemeral deployments.

3. **Pluggable Conversation Thread Stores:**
   - **Redis Thread Store (`RedisThreadStore`):** Production-grade thread session management with key pattern `th:{project}:{env}:{thread_id}` using native Redis `EXPIRE` TTLs and secondary user set index `th:sub:{project}:{env}:{issuer}:{subject}` for fast subject deletion.
   - **PostgreSQL Thread Store (`PostgresThreadStore`):** Durable SQL table (`conversation_threads`) with timestamped expiration.
   - **Memory Thread Store (`MemoryThreadStore`):** Fast in-memory thread storage for testing and standalone nodes.

4. **Runtime Pipeline Context Stage (`ContextStage`):**
   - Placed after AuthZ, Rate Limits, Guardrails, and Cache, but before Prompt Templating and Model Routing.
   - **Thread Hydration:** If `thread_id` is supplied in `ChatRequest`, verifies caller identity matches the thread's subject, retrieves prior turns from `ThreadStore`, and prepends them to `state.ChatRequest.Messages`.
   - **Memory Recall:** If `AIApp.Memory.VectorRecall` is enabled, resolves the memory provider, retrieves top relevant semantic memories for the authenticated user, and injects them as a system context prompt.
   - **Turn Appending:** On successful request or stream completion, newly exchanged turns (user question + assistant answer) are automatically appended to the active thread with renewed TTL.

5. **GDPR Subject Privacy Purge:**
   - Administrative endpoint `DELETE /v1/admin/subjects/{issuer}/{subject}` triggers `control.Service.PurgeSubjectMemory`, purging all user-scoped records from both `MemoryProvider` and `ThreadStore` in addition to logs and response caches.

## Consequences
### Positive
* Clients can conduct continuous multi-turn conversations by passing a lightweight `thread_id` without sending entire conversation transcripts repeatedly.
* User-scoped semantic memories enable contextual personalization while strictly isolating records by tenant, issuer, and subject.
* Implementors can keep all semantic memory on their own infrastructure via the HTTP provider.
* Complete GDPR/CCPA privacy compliance: subject deletions purge threads and memories across both Redis and PostgreSQL.

### Negative
* Storing large conversation threads over many turns consumes Redis RAM or database rows; bounded history and TTLs are required.
* Cosine distance operations in PostgreSQL require pgvector extension support on the target database instance.

## Alternatives Considered
* **Client-Only Message History:** Relying exclusively on client-sent history wastes mobile network bandwidth and cannot support shared cross-device sessions.
* **Global Memory Sharing:** Allowing cross-user memory recall was rejected due to strict tenant isolation and data privacy requirements.
