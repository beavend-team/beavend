# Tools, MCP & SQL Templates

Beavend allows AI models to interact with your live application systems via **Tools**. Tools execute safely on the Beavend server on behalf of authenticated end-users with strict input validation, security guards, and human-in-the-loop confirmation controls.

---

## 1. Supported Tool Executors

Beavend supports four executor architectures:

| Executor Type | Description | Best For |
|---|---|---|
| **HTTP Executor** | Invokes external REST/JSON APIs via HTTP POST/GET. | Integrating existing microservices and webhooks. |
| **MCP Executor** | Connects to external Model Context Protocol servers over stdio or HTTP/SSE. | Interoperability with modern MCP tool ecosystems. |
| **SQL Template Executor** | Executes pre-approved parameterized SQL queries with strict argument binding. | Fast, secure read access to internal databases without ORM overhead. |
| **SearXNG Web Search** | Queries an internal SearXNG metasearch instance. | Web browsing and real-time information retrieval. |

---

## 2. HTTP Tools & Input Validation

HTTP tools define an endpoint, HTTP method, headers, and a JSON Schema validating caller arguments:

```yaml
tools:
  - id: "tool-get-order"
    name: "get_order_status"
    description: "Retrieve shipping and delivery details for an order"
    side_effect: "read"
    input_schema:
      type: object
      properties:
        order_id:
          type: string
          pattern: "^ORD-[0-9]{6}$"
      required:
        - order_id
    executor:
      type: "http"
      url: "https://api.mycompany.com/orders/status"
      method: "POST"
      headers:
        X-Service-Key: "secret-vault-reference"
    identity_bindings:
      user_id: "claims.sub"
```

---

## 3. Security Protections

### Built-in SSRF Guard (Zero Setup Required)
All HTTP tool invocations are automatically routed through Beavend's custom `SafeDialer` ([`internal/tools/ssrf.go`](https://github.com/beavend/beavend/blob/master/internal/tools/ssrf.go)):
* **DNS Rebinding Defense**: Resolves DNS records prior to connection and pins verified IP addresses.
* **Denied Ranges**: Automatically rejects loopback (`127.0.0.0/8`, `::1`), RFC1918 private subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), and cloud metadata IPs (`169.254.169.254`, `metadata.google.internal`).
* **No configuration is required**—this runs as an immutable security invariant on all outgoing tool requests.

### Confused-Deputy Defense (`identity_bindings`)
When a tool needs to identify the caller, configure `identity_bindings`:
```json
{
  "user_id": "claims.sub",
  "organization_id": "claims.org_id"
}
```
* The LLM cannot forge or alter the `user_id`. Any value supplied by the LLM or client in arguments for bound keys is **strictly discarded** and overwritten with the server-verified JWT claim.

---

## 4. Parameterized SQL Template Executor

For secure database reads, use the Parameterized SQL Template executor instead of exposing raw database credentials or allowing the LLM to write arbitrary SQL:

```yaml
tools:
  - id: "tool-user-transactions"
    name: "get_recent_transactions"
    description: "Look up transactions in the last N days"
    side_effect: "read"
    input_schema:
      type: object
      properties:
        limit:
          type: integer
          maximum: 50
    identity_bindings:
      account_id: "claims.account_id"
    executor:
      type: "sql_template"
      connection_string: "postgres://readonly:secret@db.internal:5432/app"
      query: "SELECT id, amount, created_at FROM transactions WHERE account_id = $1 ORDER BY created_at DESC LIMIT $2"
      params:
        - "identity.claims.account_id"
        - "args.limit"
```

* **SQL Injection Proof**: Arguments are bound as positional database query parameters (`$1`, `$2`), never concatenated as strings.
* **Restricted Dialect**: Only `SELECT` statements are permitted.

---

## 5. Model Context Protocol (MCP) Tools

Beavend can act as an MCP Client connecting to external MCP Servers (supporting Anthropic's open Model Context Protocol):

```yaml
tools:
  - id: "tool-mcp-github"
    name: "github_mcp"
    description: "Interact with GitHub repositories via MCP"
    executor:
      type: "mcp"
      transport: "http"
      server_url: "http://mcp-github:3000/sse"
```

Beavend discovers available tool schemas from the MCP server upon snapshot compilation and bridges LLM tool calls transparently.

---

## 6. Human-in-the-Loop Confirmation Workflow

For write operations (`side_effect: write`, e.g. `transfer_funds` or `delete_record`), Beavend halts execution before calling the tool and demands client confirmation:

```mermaid
sequenceDiagram
    autonumber
    participant App as Mobile App
    participant Beavend as Beavend Runtime
    participant Redis as Valkey / Redis

    App->>Beavend: POST /v1/apps/{id}/chat (Prompt asking to transfer funds)
    Beavend->>Beavend: LLM selects write tool: transfer_funds
    Note over Beavend: Tool requires confirmation!
    Beavend->>Redis: Freeze state in pend:{project}:{req_id} (TTL: 5m)
    Beavend->>App: SSE Event: confirmation {req_id, tool_id, args, token}
    Note over App: App displays UI modal:<br/>"Confirm transfer of $50 to Alice?"
    App->>Beavend: POST /v1/apps/{id}/confirm {req_id, token}
    Beavend->>Beavend: Verify HMAC token & user identity
    Beavend->>Redis: Hydrate suspended loop state
    Beavend->>Beavend: Execute transfer_funds tool
    Beavend->>App: Final AI completion
```

This ensures write tools are never executed autonomously without the authenticated user's explicit consent.
