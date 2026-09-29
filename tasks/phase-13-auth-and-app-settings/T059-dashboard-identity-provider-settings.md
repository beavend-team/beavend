# T059 — Dashboard Identity Provider Configuration & JWT Inspector

* **Status:** Completed

## Goal
Implement a dedicated Identity Provider & Authentication settings interface in the Dashboard, enabling administrators to configure Firebase Auth, Supabase Auth, or generic OIDC parameters per environment, and test client JWTs with an interactive token inspector.

## Scope
1. **Settings Navigation Overhaul (`dashboard/src/pages/SettingsPage.tsx`):**
   - Restructure Settings into clean sub-tabs:
     - **Identity & Auth Providers** (New!)
     - **Projects & Environments**
     - **Admin Users & RBAC**
     - **Compliance & Privacy Purge**
2. **Identity & Auth Providers Tab:**
   - Provider preset selector: `Firebase Auth`, `Supabase Auth`, `Generic OIDC / JWKS`.
   - Dynamic inputs based on provider:
     - Firebase: Firebase Project ID, optional custom JWKS URL.
     - Supabase: Supabase Issuer URL, audience (default `authenticated`).
     - Generic OIDC: Issuer URL, JWKS URL, Audience list, Allowed Algorithms, Clock Skew tolerance.
   - Save button: persists config to `/v1/admin/projects/{project_id}/environments/{name}/auth`.
3. **Interactive JWT Token Inspector & Tester:**
   - Input area for a Bearer JWT token.
   - "Verify Token" action calling `/v1/admin/auth/test-token`.
   - Visual result card:
     - Status: Valid / Expired / Invalid Signature.
     - Normalized subject (`claims.sub`), issuer (`iss`), audience (`aud`).
     - Decoded claims tree with JSON viewer.
     - Quick feedback on whether the claims satisfy CEL policies or limit tiers.

## Acceptance Criteria
- [x] Administrators can view, edit, and save Identity Provider settings for the active environment.
- [x] Token inspector validates sample JWTs and displays parsed claims.
- [x] UI provides helpful documentation tips and default URLs for Firebase and Supabase.
