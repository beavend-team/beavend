# T080 - OpenAPI Contract, Control Plane Handlers, App Workspace Tabs & End-to-End Tests

* **Status:** Completed

## Goal
Expose App-level Auth and CEL Policy configuration in the OpenAPI contract, implement Control Plane CRUD handlers with validation and snapshot invalidation, move "CEL Policies" and "Identity & Auth" to dedicated App-scoped tabs in `AppWorkspace`, remove CEL and Auth inputs from the App Settings tab, and run end-to-end integration tests.

## Scope
1. **Contract (`api/openapi.yaml`):**
   - Update `AIApp` schema with optional `auth` object (`AuthConfig`).
   - Clean out all `entitlement_webhook` references.
2. **Control Plane (`internal/control/`):**
   - Update `handlers_apps.go` to parse, validate, and store `auth` configuration.
   - Trigger snapshot invalidation and pub/sub broadcast on update.
3. **Dashboard UI (`dashboard/`):**
   - Move "CEL Policies" and "Identity & Auth" from Project-level navigation to dedicated App Workspace tabs (`AppWorkspace.tsx`).
   - Implement **CEL Policies Tab**:
     - Status badge showing active policy enforcement.
     - CEL boolean expression editor with quick claim-based templates (`claims.plan in ['pro', 'team']`, `claims.email_verified == true`, etc.).
     - Interactive CEL policy sandbox testing simulated JSON claims via `api.testPolicy`.
     - Available CEL variable reference guide.
   - Implement **Identity & Auth Tab**:
     - Identity provider selector (Inherit Project Default, Firebase, Supabase, OIDC, Custom JWT, Anonymous).
     - Provider-specific configuration inputs and Anonymous Access toggle.
     - Live JWT Token Inspector verifying tokens via `api.testToken` with "Copy Claims to CEL Sandbox" workflow.
   - Clean **App Settings Tab**:
     - Remove CEL Access Policy input from general settings.
     - Remove Authentication & Identity Provider card.
     - Remove External Entitlement Webhook card.
4. **End-to-End & Integration Tests:**
   - Real flow test (`app_auth_entitlements_e2e_test.go`): Configure App with App-level auth and CEL policy -> Send request with JWT claims -> Verify policy enforcement based on `claims.plan`.

## Acceptance Criteria
- [x] OpenAPI spec validates cleanly without webhook references.
- [x] Control Plane CRUD handles app auth configuration properly.
- [x] CEL Policies and Identity & Auth are fully functional app-scoped tabs.
- [x] App Settings tab is clean with zero duplicate inputs.
- [x] End-to-end test confirms complete flow using token claims.

