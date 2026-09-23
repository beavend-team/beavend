package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cortexbase/cortexbase/internal/core"
	"github.com/cortexbase/cortexbase/internal/telemetry"
)

type contextKey string

const (
	identityContextKey contextKey = "cortexbase_identity"
	rawTokenContextKey contextKey = "cortexbase_raw_token"
)

// SnapshotResolver retrieves a Snapshot for a given project and environment.
type SnapshotResolver interface {
	Get(projectID, env string) (*core.Snapshot, bool)
}

// ContextWithIdentity returns a new context containing the validated identity.
func ContextWithIdentity(ctx context.Context, id *core.Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, id)
}

// FromContext extracts the validated identity from the context if present.
func FromContext(ctx context.Context) (*core.Identity, bool) {
	if ctx == nil {
		return nil, false
	}
	id, ok := ctx.Value(identityContextKey).(*core.Identity)
	return id, ok
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
	defaultAuthConfig *core.AuthConfig
	defaultEnv        string
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

// AuthMiddleware extracts the Bearer token, validates it against the project's AuthConfig,
// normalizes the identity into core.Identity, and injects it into the request context.
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

			// 1. Extract Bearer token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "missing authorization header", reqID)
				return
			}

			tokenStr := extractBearerToken(authHeader)
			if tokenStr == "" {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid authorization header format, expected 'Bearer <token>'", reqID)
				return
			}

			// 2. Extract Project ID and Environment
			projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
			if projectID == "" {
				writeError(w, http.StatusBadRequest, "invalid_request", "missing required X-Project-ID header", reqID)
				return
			}

			env := strings.TrimSpace(r.Header.Get("X-Environment"))
			if env == "" {
				env = cfg.defaultEnv
			}

			// 3. Resolve Snapshot from memory (zero-DB read on hot path)
			var authCfg *core.AuthConfig
			if resolver != nil {
				snap, ok := resolver.Get(projectID, env)
				if !ok {
					writeError(w, http.StatusUnauthorized, "unauthenticated", fmt.Sprintf("project %q (%s) not found", projectID, env), reqID)
					return
				}
				if snapAuth, found := snap.Auth(); found {
					authCfg = snapAuth
				}
			}

			if authCfg == nil {
				authCfg = cfg.defaultAuthConfig
			}

			if authCfg == nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication not configured for project", reqID)
				return
			}

			// 4. Verify token cryptographic signature and claims
			jwtToken, err := verifier.Verify(r.Context(), tokenStr, authCfg)
			if err != nil {
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

			// 6. Inject Identity and raw token into request Context
			ctx := ContextWithIdentity(r.Context(), id)
			ctx = ContextWithRawToken(ctx, tokenStr)
			ctx = telemetry.WithRequestID(ctx, reqID)
			ctx = telemetry.WithProjectID(ctx, projectID)
			ctx = telemetry.WithEnvironment(ctx, env)

			w.Header().Set("X-Request-Id", reqID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
