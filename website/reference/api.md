# API Reference

Beavend exposes an OpenAPI 3.1 compliant HTTP API split between public runtime endpoints (data plane) and administrative endpoints (control plane).

---

## 1. Runtime Ingress Endpoints

All runtime endpoints require a Bearer token issued by your configured identity provider (`Authorization: Bearer <USER_JWT>`).

### Chat Completion (Buffered)
`POST /v1/apps/{app_id}/chat`

Executes the AI request and returns the full completed response once all tokens and tool iterations finish.

#### Request Headers:
* `Authorization: Bearer <JWT>` *(Required)*
* `Content-Type: application/json` *(Required)*

#### Request Body:
```json
{
  "messages": [
    {
      "role": "user",
      "content": "What is the status of my order ORD-123456?"
    }
  ],
  "parameters": {
    "temperature": 0.7,
    "max_tokens": 1024
  }
}
```

#### Success Response (`200 OK`):
```json
{
  "id": "req-98fbc12a",
  "app_id": "app-support",
  "model": "smart",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Your order ORD-123456 has shipped and will arrive tomorrow."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 128,
    "completion_tokens": 42,
    "total_tokens": 170
  }
}
```

---

### Chat Completion (SSE Stream)
`POST /v1/apps/{app_id}/stream`

Streams responses token-by-token in real-time using standard **Server-Sent Events (SSE)**.

#### Request Headers:
* `Authorization: Bearer <JWT>` *(Required)*
* `Accept: text/event-stream` *(Required)*
* `Content-Type: application/json` *(Required)*

#### Event Stream Protocol:

```text
event: text_delta
data: {"content": "Your order"}

event: text_delta
data: {"content": " ORD-123456"}

event: tool_call
data: {"id": "call-1", "name": "get_order_status", "arguments": {"order_id": "ORD-123456"}}

event: tool_result
data: {"id": "call-1", "result": {"status": "shipped"}}

event: done
data: {"usage": {"total_tokens": 170}}
```

---

### Human-in-the-Loop Confirmation
`POST /v1/apps/{app_id}/confirm`

Resumes execution of a write tool that was suspended awaiting client confirmation.

#### Request Body:
```json
{
  "request_id": "req-98fbc12a",
  "confirmation_token": "hmac_signature_token_here",
  "action": "approve"
}
```

---

## 2. Health & Observability

### Liveness Probe
`GET /healthz`
* Returns `200 OK` if the process is responsive.

### Readiness Probe
`GET /readyz`
* Returns `200 OK` only when database connectivity is established and initial in-memory configuration snapshots have loaded.

### Prometheus Metrics
`GET /metrics`
* Scrapes real-time runtime metrics: request rates, active SSE streams, tool execution durations, CEL evaluation latencies, and token counters.

---

## 3. Error Taxonomy

Beavend returns standard JSON error responses:

```json
{
  "error": {
    "code": "AUTH_TOKEN_EXPIRED",
    "message": "The provided identity token has expired.",
    "status": 401
  }
}
```

### Common Error Codes:

| Code | HTTP Status | Description |
|---|---|---|
| `UNAUTHENTICATED` | 401 | Missing or invalid bearer token. |
| `AUTH_TOKEN_EXPIRED` | 401 | JWT expired past clock skew limit. |
| `POLICY_DENIED` | 403 | CEL policy evaluation denied request or tool. |
| `APP_NOT_FOUND` | 404 | AI App ID not found in current snapshot. |
| `RATE_LIMITED` | 429 | Redis rate limit or token quota exceeded. |
| `CONFIRMATION_REQUIRED` | 428 / SSE | Write tool suspended pending user approval. |
| `UPSTREAM_UNAVAILABLE` | 503 | Model provider exhausted or unreachable. |
