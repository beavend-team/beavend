# Authentication & Identity Setup

Beavend is **identity-provider agnostic** and **stores zero end-user accounts**. Instead of maintaining user tables or managing password resets, Beavend validates JSON Web Tokens (JWTs) issued by your existing identity provider on every request.

---

## 1. Supported Identity Providers

| Provider | Typical Use Case | Built-in Normalizer |
|---|---|---|
| **Firebase Auth** | Mobile (iOS / Android / Flutter) & Web | Extracts `user_id`, phone, email, tenant, and custom claims |
| **Supabase Auth** | Full-stack web & mobile with Supabase backend | Normalizes `sub`, `email`, `role`, and `app_metadata` |
| **Generic OIDC** | Auth0, Okta, Keycloak, Cognito, PocketBase | Standard OIDC RFC 7519 claim normalization |

---

## 2. Configuring an Identity Provider

In the Beavend Admin Dashboard under **Settings → Identity & Auth** (or via the Control Plane API), configure the auth provider for your project and environment:

### Firebase Auth Configuration

```json
{
  "provider": "firebase",
  "issuer": "https://securetoken.google.com/<YOUR_FIREBASE_PROJECT_ID>",
  "audience": "<YOUR_FIREBASE_PROJECT_ID>",
  "jwks_url": "https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com"
}
```

### Supabase Auth Configuration

```json
{
  "provider": "supabase",
  "issuer": "https://<YOUR_PROJECT_REF>.supabase.co/auth/v1",
  "audience": "authenticated",
  "jwks_url": "https://<YOUR_PROJECT_REF>.supabase.co/auth/v1/.well-known/jwks.json"
}
```

### Generic OIDC Configuration (e.g. Auth0, Keycloak)

```json
{
  "provider": "oidc",
  "issuer": "https://auth.yourcompany.com/",
  "audience": "https://api.yourcompany.com/ai",
  "jwks_url": "https://auth.yourcompany.com/.well-known/jwks.json"
}
```

---

## 3. Token Verification Pipeline

Whenever a client sends a request with an `Authorization: Bearer <TOKEN>` header, Beavend's identity verifier executes the following pipeline:

1. **JWKS Key Resolution**: Resolves the signing key matching the token's `kid` header. Keys are cached in-memory and automatically refreshed upon encountering an unknown key ID (key rotation).
2. **Algorithm Enforcement**: Explicitly enforces asymmetric signatures (`RS256`, `ES256`). Rejects `none` algorithm and prevents HMAC key confusion attacks.
3. **Temporal Validation**: Validates `exp` (expiration) and `nbf` (not before) with a built-in **60-second clock skew tolerance**.
4. **Issuer & Audience Check**: Verifies that `iss` and `aud` match the configured project settings.
5. **Claims Normalization**: Transforms provider-specific claims into an immutable `core.Identity` struct:
   * `identity.subject`: Unique user identifier (e.g., `user_123`).
   * `identity.issuer`: The issuing authority URL.
   * `identity.audience`: The target audience.
   * `identity.claims`: Flattened map of all user claims (e.g., `role`, `email`, `plan`, `tier`).

---

## 4. Confused-Deputy Defense with Identity Bindings

When an AI model decides to invoke a tool on behalf of a user (e.g. `getUserBalance(user_id)`), an attacker might attempt a prompt injection to force the LLM to provide someone else's `user_id`.

Beavend prevents this using **Identity Bindings** configured on the Tool definition:

```json
{
  "name": "get_user_account",
  "description": "Fetch current user account information",
  "identity_bindings": {
    "user_id": "claims.sub",
    "tenant_id": "claims.tenant_id"
  }
}
```

### Enforcement Rules:
1. When the agent loop prepares arguments for `get_user_account`, Beavend **discards** any argument supplied by the LLM or caller for `user_id` and `tenant_id`.
2. Beavend automatically injects the verified values directly from the authenticated caller's `core.Identity`.
3. If the user token does not contain the required claim, the tool call fails safely before making any downstream network call.
