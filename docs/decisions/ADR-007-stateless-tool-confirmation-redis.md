# ADR-007: Stateless Request Continuation via Redis for Tool Confirmation

## Status
Accepted

## Context
When an LLM invokes a state-mutating tool (e.g. `cancel_subscription`, `transfer_funds`), the tool may be marked as `confirmation: required`. In this scenario, the agent loop must suspend execution, prompt the user for human confirmation via the client SDK UI, and resume execution only after receiving the user's approval.

Holding HTTP/SSE connections open across load balancers while waiting minutes for a human user to review and click a button would rapidly exhaust server sockets, goroutines, and proxy connection limits. Furthermore, in a horizontally scaled cluster, the confirmation request might land on a different runtime pod.

## Decision
We decided on a **stateless tokenized continuation mechanism**:
1. When a confirmation is triggered, the runtime plane serializes the current agent loop state (accumulated messages, pending tool call, active snapshot version).
2. The state is encrypted and written to Redis under `pend:{project}:{req_id}` with a strict **5-minute TTL**.
3. The server issues a cryptographically signed HMAC token (`confirmation_token`) binding `{project, req_id, tool_id, args_hash, subject}`.
4. The server emits an SSE `confirmation` event containing the token and tool details, and immediately terminates the HTTP connection cleanly.
5. The client submits `POST /v1/apps/{id}/confirm` with the confirmation token. Any runtime instance can retrieve the state from Redis, re-verify the caller's JWT, confirm arguments were not tampered with, and execute the tool.

## Consequences
### Positive
* Runtime instances remain 100% stateless; zero sticky sessions or long-held dangling connections.
* Resilient across rolling server deployments and autoscaling events.
* Tamper-proof: token binds exact tool arguments and identity.

### Negative
* Requires Redis as an active coordination store when using confirmation or client-side tools.
* Pending state expires if the user does not respond within the 5-minute TTL.
