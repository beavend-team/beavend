# T061 — MemoryProvider & ThreadStore Domain Interfaces and Entities

## Status
Completed

## Objective
Establish pure domain entities and interfaces in `internal/core` for user-scoped semantic vector memory and server-side conversation threads with zero outward dependencies.

## Key Changes
1. `internal/core/memory.go`:
   - Domain entity `MemoryRecord`: `ID`, `ProjectID`, `Environment`, `Issuer`, `Subject`, `AIAppID`, `Content`, `Embedding`, `Metadata`, `CreatedAt`, `UpdatedAt`, `ExpiresAt`.
   - Filter criteria `MemoryFilter` and `MemorySearchQuery`.
   - Result wrapper `MemorySearchResult` with float32 cosine similarity `Score`.
   - Domain entity `Thread`: `ID`, `ProjectID`, `Environment`, `AIAppID`, `Issuer`, `Subject`, `Messages`, `Metadata`, `TTLSeconds`, `CreatedAt`, `UpdatedAt`, `ExpiresAt`.
2. `internal/core/interfaces.go`:
   - `MemoryProvider` contract: `Store`, `Recall`, `Search`, `DeleteBySubject`.
   - `ThreadStore` contract: `GetThread`, `CreateThread`, `AppendMessages`, `DeleteThread`, `DeleteThreadsBySubject`.
3. `internal/core/app.go` & `reqstate.go`:
   - Added `MemoryConfig` to `AIApp`.
   - Added `ThreadID` to `ChatRequest`, `ChatResponse`, and `StreamEvent`.
   - Added `ContextWithIdentity` and `IdentityFromContext` to `internal/core/identity.go`.
