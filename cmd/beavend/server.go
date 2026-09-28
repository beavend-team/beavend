package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/beavend/beavend/internal/config"
	"github.com/beavend/beavend/internal/runtime"
	"github.com/beavend/beavend/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ReadyChecker checks the readiness of the application (snapshot, dependencies).
type ReadyChecker interface {
	IsReady(ctx context.Context) (ready bool, details map[string]any)
}

// DefaultReadyChecker reports ready if initialized.
type DefaultReadyChecker struct {
	Role string
}

func (d *DefaultReadyChecker) IsReady(_ context.Context) (bool, map[string]any) {
	return true, map[string]any{
		"status": "ready",
		"role":   d.Role,
	}
}

// RouterOptions holds optional runtime dependencies for BuildRouter.
type RouterOptions struct {
	RuntimeHandler *runtime.Handler
	AuthMiddleware func(http.Handler) http.Handler
}

// BuildRouter configures the chi HTTP router with middlewares, health probes, and role routes.
func BuildRouter(cfg *config.Config, tel *telemetry.Telemetry, checker ReadyChecker, opts ...RouterOptions) http.Handler {
	r := chi.NewRouter()

	var rOpts RouterOptions
	if len(opts) > 0 {
		rOpts = opts[0]
	}

	// Global Middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(telemetryMiddleware)

	// Liveness & Readiness Probes (Root level, unauthenticated)
	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(checker))

	// Prometheus Metrics
	if tel != nil {
		r.Handle("/metrics", tel.MetricsHandler())
	}

	// Mount role-specific routes
	if cfg.Role == config.RoleAll || cfg.Role == config.RoleRuntime {
		mountRuntimeRoutes(r, rOpts.RuntimeHandler, rOpts.AuthMiddleware)
	}

	if cfg.Role == config.RoleAll || cfg.Role == config.RoleControl {
		mountControlRoutes(r)
	}

	return r
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleReadyz(checker ReadyChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if checker == nil {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
			return
		}

		ready, details := checker.IsReady(r.Context())
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(details)
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(details)
	}
}

func mountRuntimeRoutes(r chi.Router, handler *runtime.Handler, authMiddleware func(http.Handler) http.Handler) {
	r.Route("/v1/apps", func(apps chi.Router) {
		apps.Get("/{ai_app_id}", func(w http.ResponseWriter, r *http.Request) {
			appID := chi.URLParam(r, "ai_app_id")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":     appID,
				"status": "active",
			})
		})

		if handler != nil {
			if authMiddleware != nil {
				apps.With(authMiddleware).Post("/{ai_app_id}/chat", handler.HandleChat)
				apps.With(authMiddleware).Post("/{ai_app_id}/stream", handler.HandleStream)
				apps.With(authMiddleware).Post("/{ai_app_id}/confirm", handler.HandleConfirm)
				apps.With(authMiddleware).Post("/{ai_app_id}/tool-results", handler.HandleToolResults)
			} else {
				apps.Post("/{ai_app_id}/chat", handler.HandleChat)
				apps.Post("/{ai_app_id}/stream", handler.HandleStream)
				apps.Post("/{ai_app_id}/confirm", handler.HandleConfirm)
				apps.Post("/{ai_app_id}/tool-results", handler.HandleToolResults)
			}
		} else {
			apps.Post("/{ai_app_id}/chat", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotImplemented)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"code":    "not_implemented",
					"message": "Chat pipeline not configured",
				})
			})

			apps.Post("/{ai_app_id}/stream", func(w http.ResponseWriter, _ *http.Request) {
				// Bypass default write timeout for long-lived SSE streaming
				rc := http.NewResponseController(w)
				_ = rc.SetWriteDeadline(time.Time{})

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotImplemented)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"code":    "not_implemented",
					"message": "Stream pipeline not configured",
				})
			})

			apps.Post("/{ai_app_id}/confirm", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotImplemented)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"code":    "not_implemented",
					"message": "Confirmation handler not configured",
				})
			})

			apps.Post("/{ai_app_id}/tool-results", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotImplemented)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"code":    "not_implemented",
					"message": "Client-side tools handler not configured",
				})
			})
		}

		apps.Post("/{ai_app_id}/cancel", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "cancelled",
			})
		})
	})
}

func mountControlRoutes(r chi.Router) {
	r.Route("/v1/admin", func(admin chi.Router) {
		admin.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"control_plane": "ready",
			})
		})
	})
}

func telemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := middleware.GetReqID(r.Context())
		if reqID == "" {
			reqID = r.Header.Get("X-Request-Id")
		}

		ctx := telemetry.WithRequestID(r.Context(), reqID)
		if projID := r.Header.Get("X-Project-ID"); projID != "" {
			ctx = telemetry.WithProjectID(ctx, projID)
		}
		if env := r.Header.Get("X-Environment"); env != "" {
			ctx = telemetry.WithEnvironment(ctx, env)
		}

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		if reqID != "" {
			ww.Header().Set("X-Request-Id", reqID)
		}
		next.ServeHTTP(ww, r.WithContext(ctx))

		slog.InfoContext(ctx, "http_request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", ww.Status()),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Duration("latency", time.Since(start)),
		)
	})
}

// NewHTTPServer constructs an http.Server with configured timeouts.
func NewHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}
}
