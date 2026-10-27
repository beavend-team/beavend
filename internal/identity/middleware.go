package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/telemetry"
)

type contextKey string

const (
	identityContextKey contextKey = "beavend_identity"
	rawTokenContextKey contextKey = "beavend_raw_token" //nolint:gosec // Context key identifier, not a credential.
)

// AdminClaims represents verified administrative user claims.
type AdminClaims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

// AdminTokenValidator validates administrative session tokens.
type AdminTokenValidator interface {
	ValidateToken(tokenStr string) (*AdminClaims, error)
}

// AdminTokenValidatorFunc allows using ordinary functions as AdminTokenValidator.
type AdminTokenValidatorFunc func(tokenStr string) (*AdminClaims, error)

// ValidateToken calls f(tokenStr).
func (f AdminTokenValidatorFunc) ValidateToken(tokenStr string) (*AdminClaims, error) {
	return f(tokenStr)
}

// SnapshotResolver retrieves a Snapshot for a given project and environment.
type SnapshotResolver interface {
	Get(projectID, env string) (*core.Snapshot, bool)
}

// ContextWithIdentity returns a new context containing the validated identity.
func ContextWithIdentity(ctx context.Context, id *core.Identity) context.Context {
	return core.ContextWithIdentity(ctx, id)
}

// FromContext extracts the validated identity from the context if present.
func FromContext(ctx context.Context) (*core.Identity, bool) {
	return core.IdentityFromContext(ctx)
}

// ContextWithRawToken returns a new context containing the raw token.
func ContextWithRawToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, rawTokenContextKey, token)
}

// RawTokenFromContext extracts the raw bearer token from the context if present.
func RawTokenFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	tok, ok := ctx.Value(rawTokenContextKey).(string)
	return tok, ok
}

type middlewareConfig struct {
	defaultAuthConfig   *core.AuthConfig
	defaultEnv          string
	adminTokenValidator AdminTokenValidator
}

// MiddlewareOption configures the AuthMiddleware.
type MiddlewareOption func(*middlewareConfig)

// WithDefaultAuthConfig specifies a fallback AuthConfig if none is set in the Snapshot.
func WithDefaultAuthConfig(cfg *core.AuthConfig) MiddlewareOption {
	return func(mc *middlewareConfig) {
		mc.defaultAuthConfig = cfg
	}
}

// WithDefaultEnvironment specifies the fallback environment if X-Environment is omitted.
func WithDefaultEnvironment(env string) MiddlewareOption {
	return func(mc *middlewareConfig) {
		mc.defaultEnv = env
	}
}

// WithAdminTokenValidator configures a validator for administrative session tokens.
func WithAdminTokenValidator(v AdminTokenValidator) MiddlewareOption {
	return func(mc *middlewareConfig) {
		mc.adminTokenValidator = v
	}
}

// AuthMiddleware extracts the Bearer token, validates it against the project's AuthConfig,
// normalizes the identity into core.Identity, and injects it into the request context.
// When testing from the App Playground (X-Playground: true) or when an admin token is provided,
// the Authorization header is optional.
func AuthMiddleware(resolver SnapshotResolver, verifier *Verifier, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := &middlewareConfig{
		defaultEnv: "development",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Respect request cancellation
			if err := r.Context().Err(); err != nil {
				return
			}

			reqID := getOrGenerateRequestID(r)

			// 1. Extract Project ID and Environment
			projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
			if projectID == "" {
				writeError(w, http.StatusBadRequest, "invalid_request", "missing required X-Project-ID header", reqID)
				return
			}

			env := strings.TrimSpace(r.Header.Get("X-Environment"))
			if env == "" {
				env = cfg.defaultEnv
			}

			// Extract target AIApp ID if this is an app endpoint
			routeAppID := chi.URLParam(r, "ai_app_id")
			if routeAppID == "" {
				routeAppID = chi.URLParam(r, "id")
			}
			if routeAppID == "" {
				parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
				for i, p := range parts {
					if p == "apps" && i+1 < len(parts) {
						routeAppID = parts[i+1]
						break
					}
				}
			}

			// 2. Resolve Snapshot from memory (zero-DB read on hot path)
			var (
				authCfg *core.AuthConfig
				snap    *core.Snapshot
			)
			if resolver != nil {
				var ok bool
				snap, ok = resolver.Get(projectID, env)
				if !ok {
					writeError(w, http.StatusUnauthorized, "unauthenticated", fmt.Sprintf("project %q (%s) not found", projectID, env), reqID)
					return
				}
				// App-level AuthConfig resolution (strictly app-scoped; no project fallback)
				if routeAppID != "" {
					if app, found := snap.App(routeAppID); found && app != nil {
						authCfg = app.AuthConfig
					}
				} else if snapAuth, found := snap.Auth(); found {
					authCfg = snapAuth
				}
			}

			if authCfg == nil {
				authCfg = cfg.defaultAuthConfig
			}

			// 3. Extract Bearer token & Playground context
			authHeader := r.Header.Get("Authorization")
			tokenStr := extractBearerToken(authHeader)
			adminHeaderToken := strings.TrimSpace(r.Header.Get("X-Admin-Token"))

			// If provider is anonymous, allow access immediately
			if authCfg != nil && strings.EqualFold(authCfg.Provider, "anonymous") {
				id := &core.Identity{
					Project:     projectID,
					Environment: env,
					Issuer:      "anonymous",
					Subject:     "anonymous",
					Claims:      map[string]any{"anonymous": true},
				}
				ctx := ContextWithIdentity(r.Context(), id)
				if tokenStr != "" {
					ctx = ContextWithRawToken(ctx, tokenStr)
				}
				if snap != nil {
					ctx = core.ContextWithSnapshot(ctx, snap)
				}
				ctx = telemetry.WithRequestID(ctx, reqID)
				ctx = telemetry.WithProjectID(ctx, projectID)
				ctx = telemetry.WithEnvironment(ctx, env)

				w.Header().Set("X-Request-Id", reqID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Check for administrative session token
			var adminClaims *AdminClaims
			if cfg.adminTokenValidator != nil {
				if tokenStr != "" {
					if claims, err := cfg.adminTokenValidator.ValidateToken(tokenStr); err == nil && claims != nil {
						adminClaims = claims
					}
				}
				if adminClaims == nil && adminHeaderToken != "" {
					if claims, err := cfg.adminTokenValidator.ValidateToken(adminHeaderToken); err == nil && claims != nil {
						adminClaims = claims
					}
				}
			}

			// Playground simulation mode strictly requires verified administrative session credentials.
			// Untrusted callers cannot bypass authentication or spoof claims simply by setting X-Playground.
			isPlayground := strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Playground")), "true") && adminClaims != nil

			// If authenticated as admin and no distinct end-user simulated token was passed, authorize as admin
			if adminClaims != nil && (tokenStr == "" || tokenStr == adminHeaderToken || claimsMatchAdmin(cfg.adminTokenValidator, tokenStr)) {
				id := &core.Identity{
					Project:     projectID,
					Environment: env,
					Issuer:      "beavend:admin",
					Subject:     adminClaims.UserID,
					Claims: map[string]any{
						"sub":        adminClaims.UserID,
						"email":      adminClaims.Email,
						"role":       adminClaims.Role,
						"roles":      []string{adminClaims.Role, "admin"},
						"admin":      true,
						"playground": true,
					},
				}
				ctx := ContextWithIdentity(r.Context(), id)
				if tokenStr != "" {
					ctx = ContextWithRawToken(ctx, tokenStr)
				}
				if snap != nil {
					ctx = core.ContextWithSnapshot(ctx, snap)
				}
				ctx = telemetry.WithRequestID(ctx, reqID)
				ctx = telemetry.WithProjectID(ctx, projectID)
				ctx = telemetry.WithEnvironment(ctx, env)

				w.Header().Set("X-Request-Id", reqID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// If Authorization token is missing
			if tokenStr == "" {
				// Anonymous execution allowed
				if authCfg != nil && authCfg.AllowAnonymous {
					id := &core.Identity{
						Project:     projectID,
						Environment: env,
						Issuer:      "anonymous",
						Subject:     "anonymous",
						Claims:      map[string]any{"anonymous": true},
					}
					ctx := ContextWithIdentity(r.Context(), id)
					if snap != nil {
						ctx = core.ContextWithSnapshot(ctx, snap)
					}
					ctx = telemetry.WithRequestID(ctx, reqID)
					ctx = telemetry.WithProjectID(ctx, projectID)
					ctx = telemetry.WithEnvironment(ctx, env)

					w.Header().Set("X-Request-Id", reqID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				// Authorization header is optional in app playground
				if isPlayground || adminClaims != nil {
					id := &core.Identity{
						Project:     projectID,
						Environment: env,
						Issuer:      "beavend:playground",
						Subject:     "playground-user",
						Claims: map[string]any{
							"sub":        "playground-user",
							"email":      "playground@beavend.local",
							"role":       "admin",
							"roles":      []string{"developer", "admin"},
							"playground": true,
							"anonymous":  true,
						},
					}
					if adminClaims != nil {
						id.Issuer = "beavend:admin"
						id.Subject = adminClaims.UserID
						id.Claims["sub"] = adminClaims.UserID
						id.Claims["email"] = adminClaims.Email
						id.Claims["role"] = adminClaims.Role
						id.Claims["roles"] = []string{adminClaims.Role, "admin"}
					}
					ctx := ContextWithIdentity(r.Context(), id)
					if snap != nil {
						ctx = core.ContextWithSnapshot(ctx, snap)
					}
					ctx = telemetry.WithRequestID(ctx, reqID)
					ctx = telemetry.WithProjectID(ctx, projectID)
					ctx = telemetry.WithEnvironment(ctx, env)

					w.Header().Set("X-Request-Id", reqID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				if authHeader == "" {
					writeError(w, http.StatusUnauthorized, "unauthenticated", "missing authorization header", reqID)
					return
				}
				writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid authorization header format, expected 'Bearer <token>'", reqID)
				return
			}

			// 3b. Check if token is an App API Key (e.g. bv_app_... or bv_...)
			if strings.HasPrefix(tokenStr, "bv_app_") || strings.HasPrefix(tokenStr, "bv_") {
				h := sha256.Sum256([]byte(tokenStr))
				keyHash := hex.EncodeToString(h[:])

				if snap == nil {
					writeError(w, http.StatusUnauthorized, "unauthenticated", "configuration snapshot not found", reqID)
					return
				}

				appKey, found := snap.AppAPIKeyByHash(keyHash)
				if !found || appKey == nil {
					writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid or revoked app API key", reqID)
					return
				}

				if appKey.ExpiresAt != nil && appKey.ExpiresAt.Before(time.Now()) {
					writeError(w, http.StatusUnauthorized, "unauthenticated", "app API key has expired", reqID)
					return
				}

				// Check route path scoping: if the route is for an app endpoint, verify app ID matches
				if routeAppID != "" && routeAppID != appKey.AppID {
					writeError(w, http.StatusForbidden, "forbidden", fmt.Sprintf("app API key is only authorized for app %q, not %q", appKey.AppID, routeAppID), reqID)
					return
				}

				// Construct normalized Identity for the App API Key
				id := &core.Identity{
					Project:     projectID,
					Environment: env,
					Audience:    appKey.AppID,
					Claims: map[string]any{
						"app_id":   appKey.AppID,
						"key_id":   appKey.ID,
						"key_name": appKey.Name,
						"scope":    "app",
					},
				}

				assertedSubject := strings.TrimSpace(r.Header.Get("X-Beavend-Subject"))
				if assertedSubject != "" {
					if !appKey.MayAssertSubject {
						writeError(w, http.StatusForbidden, "forbidden", "app API key is not permitted to assert subject", reqID)
						return
					}

					id.Subject = assertedSubject

					// Issuer: app.AuthConfig.Issuer or "beavend:bridge:" + appKey.AppID
					app, _ := snap.App(appKey.AppID)
					if app != nil && app.AuthConfig != nil && strings.TrimSpace(app.AuthConfig.Issuer) != "" {
						id.Issuer = app.AuthConfig.Issuer
					} else {
						id.Issuer = "beavend:bridge:" + appKey.AppID
					}

					// Filter X-Beavend-Claims against appKey.AllowedClaims, stripping reserved claims
					claimsHeader := strings.TrimSpace(r.Header.Get("X-Beavend-Claims"))
					if claimsHeader != "" {
						var parsedClaims map[string]any
						if err := json.Unmarshal([]byte(claimsHeader), &parsedClaims); err != nil {
							writeError(w, http.StatusBadRequest, "invalid_request", "malformed X-Beavend-Claims JSON header", reqID)
							return
						}
						allowedSet := make(map[string]bool, len(appKey.AllowedClaims))
						for _, c := range appKey.AllowedClaims {
							allowedSet[c] = true
						}
						reservedClaims := map[string]bool{
							"admin":      true,
							"playground": true,
							"sub":        true,
							"iss":        true,
							"aud":        true,
							"scope":      true,
						}
						for k, v := range parsedClaims {
							if !reservedClaims[k] && allowedSet[k] {
								id.Claims[k] = v
							}
						}
					}
				} else {
					id.Subject = appKey.AppID
					id.Issuer = "beavend:app_key"
					id.Claims["machine"] = true
				}

				ctx := ContextWithIdentity(r.Context(), id)
				// Invariant: NEVER populate RawToken from an AppAPIKey
				ctx = ContextWithRawToken(ctx, "")
				ctx = core.ContextWithSnapshot(ctx, snap)
				ctx = telemetry.WithRequestID(ctx, reqID)
				ctx = telemetry.WithProjectID(ctx, projectID)
				ctx = telemetry.WithEnvironment(ctx, env)

				w.Header().Set("X-Request-Id", reqID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if authCfg == nil || authCfg.Provider == "" {
				if isPlayground {
					sub, claims := parseSimulatedToken(tokenStr)
					id := &core.Identity{
						Project:     projectID,
						Environment: env,
						Issuer:      "beavend:playground_simulated",
						Subject:     sub,
						Claims:      claims,
					}
					ctx := ContextWithIdentity(r.Context(), id)
					ctx = ContextWithRawToken(ctx, tokenStr)
					if snap != nil {
						ctx = core.ContextWithSnapshot(ctx, snap)
					}
					ctx = telemetry.WithRequestID(ctx, reqID)
					ctx = telemetry.WithProjectID(ctx, projectID)
					ctx = telemetry.WithEnvironment(ctx, env)

					w.Header().Set("X-Request-Id", reqID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication not configured for project", reqID)
				return
			}

			// 4. Verify token cryptographic signature and claims
			jwtToken, err := verifier.Verify(r.Context(), tokenStr, authCfg)
			if err != nil {
				// In playground, allow plain mock subjects or mock JSON claims (e.g. {"sub":"user1"})
				// only if the token is not a signed JWT (has fewer than 2 dots) or no provider is configured.
				// If a real JWT is provided to test an active auth provider, verification errors (e.g. expired, bad signature) must be returned!
				isJWTFormat := strings.Count(strings.TrimSpace(tokenStr), ".") == 2
				if isPlayground && !isJWTFormat {
					sub, claims := parseSimulatedToken(tokenStr)
					id := &core.Identity{
						Project:     projectID,
						Environment: env,
						Issuer:      "beavend:playground_simulated",
						Subject:     sub,
						Claims:      claims,
					}
					ctx := ContextWithIdentity(r.Context(), id)
					ctx = ContextWithRawToken(ctx, tokenStr)
					if snap != nil {
						ctx = core.ContextWithSnapshot(ctx, snap)
					}
					ctx = telemetry.WithRequestID(ctx, reqID)
					ctx = telemetry.WithProjectID(ctx, projectID)
					ctx = telemetry.WithEnvironment(ctx, env)

					w.Header().Set("X-Request-Id", reqID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error(), reqID)
				return
			}

			// 5. Normalize identity
			normalizer := NewNormalizer(authCfg.Provider)
			id, err := normalizer.Normalize(r.Context(), jwtToken, projectID, env)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated", fmt.Sprintf("normalize identity: %v", err), reqID)
				return
			}

			// 6. Inject Identity, Snapshot, and raw token into request Context
			ctx := ContextWithIdentity(r.Context(), id)
			ctx = ContextWithRawToken(ctx, tokenStr)
			if snap != nil {
				ctx = core.ContextWithSnapshot(ctx, snap)
			}
			ctx = telemetry.WithRequestID(ctx, reqID)
			ctx = telemetry.WithProjectID(ctx, projectID)
			ctx = telemetry.WithEnvironment(ctx, env)

			w.Header().Set("X-Request-Id", reqID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func claimsMatchAdmin(validator AdminTokenValidator, token string) bool {
	if validator == nil || token == "" {
		return false
	}
	claims, err := validator.ValidateToken(token)
	return err == nil && claims != nil
}

func parseSimulatedToken(tokenStr string) (string, map[string]any) {
	claims := make(map[string]any)
	trimmed := strings.TrimSpace(tokenStr)

	// Try parsing JSON format: {"sub":"alice","plan":"pro"}
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if err := json.Unmarshal([]byte(trimmed), &claims); err == nil {
			if sub, ok := claims["sub"].(string); ok && sub != "" {
				return sub, claims
			}
			return "simulated-user", claims
		}
	}

	// Try parsing JWT payload (header.payload.signature)
	parts := strings.Split(trimmed, ".")
	if len(parts) >= 2 {
		payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err == nil {
			if err := json.Unmarshal(payloadBytes, &claims); err == nil {
				if sub, ok := claims["sub"].(string); ok && sub != "" {
					return sub, claims
				}
				return "simulated-user", claims
			}
		}
	}

	// Plain subject string
	claims["sub"] = trimmed
	claims["simulated"] = true
	return trimmed, claims
}

func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func getOrGenerateRequestID(r *http.Request) string {
	if reqID := telemetry.RequestIDFromContext(r.Context()); reqID != "" {
		return reqID
	}
	if reqID := r.Header.Get("X-Request-Id"); reqID != "" {
		return reqID
	}
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return "req_" + hex.EncodeToString(bytes)
}

func writeError(w http.ResponseWriter, statusCode int, code, message, reqID string) {
	if reqID != "" {
		w.Header().Set("X-Request-Id", reqID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       code,
		"message":    message,
		"request_id": reqID,
	})
}
