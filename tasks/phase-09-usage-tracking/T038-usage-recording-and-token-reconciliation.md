# T038 — Runtime Usage Recording & Token Reconciliation

* **Status:** Completed

## Goal
Integrate usage recording and post-request token reconciliation into the runtime handler for both unary chat and streaming completions.

## Depends On
- T037 (Batch Writer)
- Phase 7 (Runtime Pipeline)
- Phase 5 (Rate Limiting)

## Scope
1. **Usage Recording Extractor (`internal/usage/extractor.go`):**
   - Extract operational metadata from `core.ReqState`:
     - Identifiers: `RequestID`, `ProjectID`, `Environment`, `AIAppID`, User identity (`Subject`, `Issuer`).
     - Performance: `Latency` (`time.Since(StartTime)`), `IsStreaming`.
     - Model: Logical model requested, actual resolved deployment and adapter.
     - Tokens & Cost: `InputTokens`, `OutputTokens`, `TotalTokens`, `CostUSD`.
     - Status: Finished status (`success`, `error`, `cancelled`), HTTP status code, `ErrorCode`, `ErrorMessage`.
     - Tool calls: List of executed tools (name, latency, status).
2. **Post-Request Hook in Runtime Handler:**
   - In `HandleChat`: after sending HTTP response, asynchronously emit event to `UsageTracker`.
   - In `HandleStream`: after SSE stream closes (either clean `done` or client disconnect/error), calculate stream duration, total tokens streamed, and asynchronously emit event.
   - In `HandleConfirm` and `HandleToolResults`: record tool resumption completions.
3. **Asynchronous Token Reconciliation:**
   - If pre-request tokens were estimated in `ReqState.Metadata["estimated_tokens"]` and `limit_user_key` is present:
     - Invoke `LimitStore.ReconcileTokens(ctx, userKey, estimatedTokens, actualTokens)` asynchronously.
     - Credit back over-estimated tokens or debit extra under-estimated tokens.
4. Unit tests covering unary and streaming usage emission, error state capture, and reconciliation invocation.

## Acceptance Criteria
- [ ] Both unary and streaming requests produce valid usage and log events.
- [ ] Client disconnect mid-stream still emits partial usage and reconciles limits.
- [ ] Token reconciliation is triggered with correct delta.
