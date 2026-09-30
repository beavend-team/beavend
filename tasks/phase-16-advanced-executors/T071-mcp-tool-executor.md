# T071 — MCP (Model Context Protocol) Tool Executor

## Status
Completed

## Objective
Implement an SSRF-safe Model Context Protocol (MCP) executor conforming to JSON-RPC 2.0 specifications over HTTP/SSE, allowing Beavend to invoke external MCP tools with user identity bindings and auth credentials.

## Requirements & Scope
1. Implement `tools.MCPExecutor` satisfying `core.Executor`:
   - Handles `tools/call` JSON-RPC 2.0 requests:
     ```json
     {
       "jsonrpc": "2.0",
       "id": "<request_id>",
       "method": "tools/call",
       "params": {
         "name": "<tool_name>",
         "arguments": { ... }
       }
     }
     ```
   - Supports downstream authentication (headers, bearer tokens, or forwarded user JWT).
   - Enforces SSRF defense via `SafeDialer` (denies loopback, private ranges, link-local, cloud metadata).
   - Configurable timeout and retry bounds.
   - Extracts result content from MCP text, image, or resource items:
     ```json
     {
       "jsonrpc": "2.0",
       "id": "<request_id>",
       "result": {
         "content": [
           { "type": "text", "text": "..." }
         ],
         "isError": false
       }
     }
     ```
   - Normalizes errors and returns `core.ToolResult`.
2. Unit tests covering:
   - Successful tool call and response extraction.
   - MCP protocol error handling (`isError: true` and JSON-RPC error objects).
   - Downstream authentication injection.
   - Timeout and SSRF rejection.
