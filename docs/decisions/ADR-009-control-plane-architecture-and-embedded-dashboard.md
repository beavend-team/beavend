# ADR-009: Control Plane Architecture and Embedded Dashboard

## Status
Accepted

## Context
Beavend requires an administrative management plane to configure AI applications, versioned prompts, models, external tools, guardrails, and authorization policies, as well as inspect request telemetry, usage rollups, and audit logs. The management plane must maintain strict security isolation from the high-throughput, end-user runtime data plane. Furthermore, Beavend is designed as a single self-hostable binary where operators should not be forced to run a separate Node.js server or container to access the administrative dashboard.

## Decision
1. **Administrative Authentication & RBAC:**
   - Admin authentication is completely separate from end-user JWT validation.
   - Admin accounts are stored in PostgreSQL (`admin_users`) with bcrypt password hashing.
   - Sessions are managed via signed HMAC tokens issued upon authentication, supported both in the `Authorization: Bearer <admin-token>` header and an `HttpOnly`, `SameSite=Lax` cookie (`beavend_admin_session`).
   - Role-Based Access Control (RBAC) enforces three standard roles:
     - `viewer`: Read-only access to configuration, metrics, and logs.
     - `editor`: Can create, update, and publish applications, prompts, tools, and policies.
     - `owner`: Full privileges, including environment provisioning, admin user management, provider credentials, and privacy purge operations.
2. **Synchronous Audit Logging:**
   - Every mutating administrative operation across `/v1/admin/*` records a persistent audit log entry (`audit_log` table) capturing tenant context, operator identity, action, resource reference, and change payload.
3. **Atomic Revision Invalidation:**
   - Control plane mutation handlers do not write directly to runtime memory. Instead, on successful PostgreSQL commit, the handler increments `project_revisions` and dispatches an invalidation event via Redis pub/sub (with PostgreSQL `LISTEN/NOTIFY` fallback).
   - Horizontal runtime nodes receive the notification and compile a fresh in-memory snapshot atomically, maintaining the core zero-DB-read hot-path invariant.
4. **Embedded Static Dashboard SPA:**
   - The admin UI is built with React 18, TypeScript, Vite, and Tailwind CSS.
   - The compiled static production build (`dashboard/dist`) is embedded directly into the Go binary using Go's standard library `embed.FS`.
   - The server routes `/dashboard/*` to the embedded file system with SPA fallback to `index.html`.

## Consequences
### Positive
* Single binary distribution: deployment requires only the compiled Go executable (or Docker image) without needing a separate frontend container.
* Strict security posture: end-user tokens cannot authenticate to administrative endpoints, and all configuration changes leave an immutable audit trail.
* Architectural purity: runtime plane remains stateless and decoupled from administrative database mutations.

### Negative
* Binary size increases moderately (~1-3 MB) due to embedded dashboard HTML/JS/CSS assets.
* Frontend rebuild requires running Vite build before Go compilation in release workflows.

## Alternatives Considered
* **Separate Frontend Container / Service:** Rejected because self-hosters want simple single-binary or single-container deployments.
* **Server-Rendered HTML Templates:** Rejected because modern AI app management (CEL rule builders, JSON schema editors, real-time usage charts) benefits significantly from a dynamic React client application.
