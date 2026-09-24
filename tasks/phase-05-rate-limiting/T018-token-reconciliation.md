# T018 — Token Reservation and Post-Request Reconciliation [Completed]

* **Status:** Completed

## Goal
Implement optimistic token reservation and asynchronous post-request token reconciliation in `internal/limits`.

## Depends On
- T016 (Redis Limits)

## Scope
* Implement token estimation utility:
  * Upper bound token estimate before request: input token estimate + requested `max_tokens` (or default upper bound).
  * Conservative character-to-token ratio or message counter when tokenizer is not available.
* Implement `ReconcileTokens` in `LimitStore`:
  * Computes delta = `actualTokens - estimatedTokens`.
  * If delta < 0, refunds the excess reserved tokens from the active window counters.
  * If delta > 0, debits the additional tokens consumed.
  * If delta == 0, no-op.
* Unit tests for:
  * Exact estimate vs actual tokens (no change).
  * Overestimation refund (e.g. reserved 1000, actual 200 -> refunds 800).
  * Underestimation additional debit (e.g. reserved 500, actual 700 -> debits 200).
  * Counter flooring at zero (refund cannot reduce counter below zero).

## Acceptance Criteria
- [x] Atomic credit/debit adjustment across all active token windows for the user key.
- [x] Safe against negative counter values on refunds.
- [x] Unit tests pass with race detector enabled.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/limits/...
```
