package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Middleware creates an HTTP middleware that authenticates incoming admin requests.
func Middleware(tm *TokenManager, userStore ...UserStore) func(http.Handler) http.Handler {
	var store UserStore
	if len(userStore) > 0 {
		store = userStore[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CSRF protection for cookie-authenticated state-changing requests
			if r.Header.Get("Authorization") == "" && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
				fetchSite := r.Header.Get("Sec-Fetch-Site")
				if fetchSite != "" {
					if fetchSite != "same-origin" && fetchSite != "none" {
						writeError(w, http.StatusForbidden, "forbidden", "cross-site cookie request rejected")
						return
					}
				} else {
					// Fallback CSRF verification for browsers that omit Sec-Fetch-Site
					origin := r.Header.Get("Origin")
					if origin == "" {
						origin = r.Header.Get("Referer")
					}
					if origin != "" {
						if u, err := url.Parse(origin); err == nil && u.Host != "" {
							if !strings.EqualFold(u.Host, r.Host) {
								writeError(w, http.StatusForbidden, "forbidden", "cross-origin cookie request rejected")
								return
							}
						}
					}
				}
			}

			tokenStr := extractToken(r)
			if tokenStr == "" {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "admin authentication required")
				return
			}

			claims, err := tm.ValidateToken(tokenStr)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
				return
			}

			adminUser := &AdminUser{
				ID:        claims.UserID,
				Email:     claims.Email,
				Role:      claims.Role,
				CreatedAt: time.Unix(claims.IssuedAt, 0),
				UpdatedAt: time.Unix(claims.IssuedAt, 0),
			}

			if store != nil {
				dbUser, err := store.GetAdminUserByID(r.Context(), claims.UserID)
				if err != nil {
					writeError(w, http.StatusUnauthorized, "unauthenticated", "admin account not found or revoked")
					return
				}
				if dbUser.IsDisabled {
					writeError(w, http.StatusForbidden, "forbidden", "admin account is disabled")
					return
				}
				adminUser.Role = Role(dbUser.Role)
				adminUser.Email = dbUser.Email
			}

			ctx := WithAdminUser(r.Context(), adminUser)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole enforces that the authenticated admin user possesses at least minRole.
func RequireRole(minRole Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := GetAdminUser(r.Context())
			if !ok || u == nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "admin user context not found")
				return
			}

			if !u.Role.Can(minRole) {
				writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions for this resource")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractToken(r *http.Request) string {
	// 1. Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	// 2. Check Cookie
	if cookie, err := r.Cookie(CookieSessionName); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	return ""
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}
