# ADR-015: SearXNG Meta-Search Engine Integration & App-Scoped API Keys

## Status
Accepted

## Context
1. **Live Web Search Capability**:
   Modern LLM agents frequently require real-time information retrieval (breaking news, academic papers, recent code documentation, up-to-date factual data) beyond the cutoff date of their base models. Proprietary search APIs (Google Custom Search, Bing Web Search, Tavily, Brave Search) introduce third-party subscription costs, rate limits, tracking, and external dependencies.
   SearXNG is an open-source, privacy-respecting, metasearch engine capable of aggregating search results from over 70 search engines without tracking user profiles or query IP addresses.

2. **License Isolation (AGPL-3.0 vs MIT/Apache 2.0)**:
   SearXNG is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0). Beavend is released under permissive open-source licenses (MIT / Apache 2.0). Linking, embedding, or distributing SearXNG code inside the Beavend Go binary would subject Beavend to AGPL copyleft requirements. Clean separation via network-only communication is required.

3. **Resilience & Graceful Degradation**:
   External search engines frequently encounter rate limits (HTTP 429), CAPTCHAs, bot blocks, upstream connection failures, or service restarts. A failure in web search must never panic the runtime, break active SSE streams, or terminate the agent loop abruptly.

4. **Per-App Configuration & App-Scoped Authorization**:
   Search capabilities should not be globally forced across all applications; instead, each AI App must be able to independently toggle web search on/off and configure search parameters (max results, categories, safe search).
   Additionally, server-to-server systems (e.g. backend background workers, data pipelines, CRON jobs) need to invoke Beavend AI Apps directly without minting end-user JWTs from Firebase/Supabase/OIDC identity providers. Authorization keys must be scoped strictly to individual AI Apps with SHA-256 hash validation against in-memory snapshots (zero database reads on the hot path).

5. **Multi-Environment Routing Transparency**:
   Implementors frequently test across `development`, `staging`, and `production`. Integration snippets must dynamically reflect the active project, environment, and appropriate authorization mode (`X-Environment`, `X-Project-ID`, App API Key vs User JWT).

## Decision

1. **Dockerized Architectural Isolation**:
   - SearXNG is provisioned as an autonomous Docker container (`searxng/searxng:latest`) within the official `docker-compose.yml` bundle.
   - SearXNG runs on an internal Docker bridge network (`http://searxng:8080`), configured via `deploy/docker/searxng/settings.yml` with `formats: [html, json]`, a generated secret key, and internal limiter disabled for container-to-container calls.
   - Beavend communicates exclusively via standard HTTP REST API queries (`/search?q=...&format=json`). No shared memory, binary linkage, or Go library importation exists between Beavend and SearXNG, preserving Beavend's MIT/Apache 2.0 licensing integrity.

2. **SearXNG Client & Built-in Tool Executor**:
   - Implement `tools.SearXNGClient` in `internal/tools/searxng.go` with configurable base URL, timeout, and health checking (`/healthz`).
   - Implement `tools.SearXNGExecutor` conforming to `core.Executor`.
   - Implement `tools.BuiltinSearchToolDefinition()` providing the standard `web_search` function schema:
     ```json
     {
       "name": "web_search",
       "description": "Search the public web for real-time information, recent events, facts, or documentation.",
       "parameters": {
         "type": "object",
         "properties": {
           "query": { "type": "string", "description": "The search query term" },
           "categories": { "type": "string", "description": "Optional search category: general, science, it, news" }
         },
         "required": ["query"]
       }
     }
     ```
   - Auto-inject `web_search` into the agent loop in `internal/runtime/agent/loop.go` when `app.Search.Enabled` is true.

3. **Graceful Degradation & Edge Case Handling**:
   - Connection Refused / Inactive: Returns structured error notification to the LLM agent: *"Search service is currently unavailable. Proceed with internal knowledge."*
   - HTTP 429 / 5xx / Timeouts: Catches HTTP status codes and context deadlines, returning diagnostic messages rather than terminating the pipeline.
   - Empty Results: Returns *"No relevant web results found for query."* allowing the model to adapt or rephrase.
   - Empty Query: Rejects early with explanatory text without making an upstream network call.

4. **App-Scoped API Keys (`AppAPIKey`)**:
   - Create `app_api_keys` table with `id`, `app_id`, `project_id`, `environment`, `name`, `key_prefix`, `key_hash`, `created_at`, `last_used_at`, and `expires_at`.
   - Keys use the standard prefix `bv_app_sec_<24-random-bytes-base64>`. Only the SHA-256 hash is persisted.
   - **Zero DB Reads on Hot Path**: The Snapshot Compiler loads active `app_api_keys` into `snap.AppAPIKeys` (map keyed by SHA-256 hex string).
   - In `internal/identity/middleware.go`, bearer tokens beginning with `bv_app_` are hashed and validated in-memory against `snap.AppAPIKeys`. The middleware verifies that the key's `AppID` strictly matches the URL path (`/v1/apps/{id}/*`), preventing cross-app privilege escalation.
   - Generates an in-memory `core.Identity` (`Issuer: "beavend:app_key"`, `Subject: appKey.AppID`) allowing the request to proceed through rate limits and policy stages seamlessly.

5. **Control Plane & Admin Dashboard**:
   - Add `POST /v1/admin/apps/{id}/keys`, `GET /v1/admin/apps/{id}/keys`, and `DELETE /v1/admin/apps/{id}/keys/{key_id}`.
   - Add `GET /v1/admin/search/status` (checks SearXNG ping and JSON capability) and `POST /v1/admin/search/test` (interactive query console).
   - Add "Search Engine (SearXNG)" permanent tab in Tools Registry with live health badges and interactive tester.
   - Add "Web Search" configuration card in AI App Workspace Settings (toggle, max results, category, safe search).
   - Add "API Keys" tab in AI App Workspace with key generation modal (one-time secret reveal) and revocation table.
   - Enhance Quick Integration snippets to dynamically reflect active `X-Environment` and `X-Project-ID` headers with dual App API Key / User JWT examples.

## Consequences

### Positive
- Fully self-hosted, private web search capability with zero external subscription costs or query logging.
- Strict license isolation guarantees Beavend remains completely unencumbered by AGPL-3.0.
- LLM inference streams never crash when SearXNG is unavailable or degraded.
- Backend services and background workers can call AI Apps directly via secure, scoped App API Keys without creating fake user JWTs.
- Zero database queries added to the runtime request hot path.

### Negative / Trade-offs
- Docker Compose memory footprint slightly increased (~150MB for the SearXNG Python container).
- Upstream search engines may occasionally rate-limit public scraping if SearXNG is operated from high-density shared IP addresses (mitigated via category selection, engines configuration, and proxying).

## Alternatives Considered
- **Linking a Go search library directly**: Would violate Beavend's data-agnostic, modular monolith principles and risk licensing contamination.
- **Requiring proprietary search APIs (Tavily/SerpAPI)**: Rejected because Beavend's core mission is self-hostability with provider independence and zero mandatory external SaaS subscriptions.
- **Validating App API Keys in PostgreSQL on every request**: Violates Beavend's core architectural invariant of zero database reads on the runtime hot path.
