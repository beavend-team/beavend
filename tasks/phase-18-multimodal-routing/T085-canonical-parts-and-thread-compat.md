# T085 - Canonical Message Parts & Model-Agnostic Threads

* **Status:** Completed

## Goal
Make sessions survive model switches (ADR-017 §7).

## Scope
1. Define `Part` types in `internal/core`; store thread messages as parts (migration for `threads`/messages content).
2. Media reference storage interface (`MediaStore`) with local/S3 implementations; digests and signed URLs.
3. Adapter boundary translation in litellm/openaicompat adapters.
4. Context stage (`stages/context.go`): when the routed model lacks a modality, substitute a placeholder/caption; enforce the context window per resolved model.
5. Tests: image turn → text-only model turn → vision model turn.

## Acceptance Criteria
- [x] Switching models mid-thread works.
- [x] No provider-native formats persisted.
