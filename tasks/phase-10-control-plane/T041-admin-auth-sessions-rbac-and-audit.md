# T041 — Admin Authentication, Sessions, RBAC & Audit Logging

* **Status:** Completed

## Goal
Implement administrative user authentication, session / bearer token management, role-based access control (RBAC), and persistent audit logging for the control plane.

## Depends On
- Phase 1 (Foundation)
- Phase 2 (Persistent Storage & Migrations)

## Scope
1. **Database Queries (`internal/store/queries/admin_users.sql`, `internal/store/queries/audit_log.sql`):**
   - `admin_users`: `CreateAdminUser`, `GetAdminUserByEmail`, `GetAdminUserByID`, `ListAdminUsers`, `UpdateAdminUserPassword`, `UpdateAdminUserRole`, `DeleteAdminUser`.
   - `audit_log`: `CreateAuditLogEntry`, `ListAuditLogsByProjectEnv`, `ListAuditLogs`.
   - Run `sqlc generate`.
2. **Admin Auth & RBAC Domain (`internal/control/auth`):**
   - Password hashing and verification using `golang.org/x/crypto/bcrypt`.
   - Role taxonomy: `owner`, `editor`, `viewer`.
   - Permissions hierarchy:
     - `viewer`: Read-only access to configs, usage, and logs.
     - `editor`: Can create, update, and delete configs (apps, prompts, models, tools, policies).
     - `owner`: Can manage environments, projects, admin users, credentials, and privacy purge operations.
   - Admin Session / Token manager:
     - Cryptographically signed HMAC / SHA-256 session token with expiration (configurable TTL, e.g. 24h).
     - Supports both `Authorization: Bearer <admin-token>` and HTTP-only cookie `beavend_admin_session`.
   - Admin Auth HTTP Middleware:
     - Extracts and validates token/cookie.
     - Injects `AdminUser` context.
     - Enforces required minimum role (`RequireRole(role)`).
3. **Audit Logger (`internal/control/audit`):**
   - Helper to record structured audit log entries upon any mutating action:
     `project_id`, `environment`, `user_id`, `action`, `resource_type`, `resource_id`, `payload`.
   - Asynchronous or transactional write to `audit_log` table.
4. **Authentication Endpoints:**
   - `POST /v1/admin/auth/login` (email + password -> returns token + sets session cookie)
   - `POST /v1/admin/auth/logout` (clears cookie)
   - `GET /v1/admin/auth/me` (returns current authenticated admin user profile and role)
   - `GET /v1/admin/users`, `POST /v1/admin/users`, `DELETE /v1/admin/users/{id}` (owner only)
   - `GET /v1/admin/audit-logs` (filtered by project, env, resource, pagination)
5. **Initial Admin Seeding:**
   - Automatic seeding of default admin if `admin_users` table is empty via environment variables (`BEAVEND_ADMIN_EMAIL`, `BEAVEND_ADMIN_PASSWORD`).

## Acceptance Criteria
- [ ] Sqlc queries generate clean, type-safe Go code for `admin_users` and `audit_log`.
- [ ] Passwords stored using bcrypt with appropriate cost factor.
- [ ] Login returns valid token and sets HTTP-only cookie; logout clears cookie.
- [ ] RBAC middleware permits/denies access according to `viewer`, `editor`, `owner` roles.
- [ ] Mutating actions generate persistent audit log entries.
- [ ] Comprehensive unit tests with >85% coverage.
