# T034: Suspended State Continuation & HMAC Confirmation Tokens

## Status
Completed

## Context & Objectives
In accordance with ADR-007 and PRD §18.2:
When a tool invocation requires human confirmation (or execution on the client device), the request must suspend without holding open network connections or pinning in-memory state.

### Key Requirements:
1. **Suspended State in Redis**:
   * Key format: `pend:{project_id}:{request_id}`
   * TTL: Strict 5 minutes (300 seconds).
   * Payload: Serialized and encrypted/signed state containing `project_id`, `environment`, `request_id`, `ai_app_id`, `subject`, `issuer`, `messages`, `pending_tool_call`, and `snapshot_revision`.
2. **HMAC Confirmation Token**:
   * Cryptographically signed HMAC-SHA256 token binding:
     - `project_id`
     - `request_id`
     - `tool_id`
     - `args_hash` (SHA-256 of canonical tool arguments JSON)
     - `subject` (user ID)
     - `expires_at` (epoch timestamp)
   * The token is URL-safe base64 encoded.
3. **Continuation Validation**:
   * Token signature check using master/HMAC key.
   * Expiration check.
   * Subject match: the user resuming the request MUST match the user who initiated it.
   * Arguments integrity: tool arguments executed MUST match the hash signed in the token.

## Deliverables
* `internal/tools/continuation.go`: State serialization, Redis storage, HMAC token generator and validator.
* `internal/tools/continuation_test.go`: Unit tests for suspension, resumption, tampering, subject mismatch, and expiration.
