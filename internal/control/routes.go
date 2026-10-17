package control

import (
	"encoding/json"
	"net/http"

	"github.com/beavend/beavend/internal/control/auth"
	chi "github.com/go-chi/chi/v5"
)

// maxBodySizeMiddleware limits the maximum request body size read by downstream handlers.
func maxBodySizeMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// MountRoutes registers all administrative and management endpoints under the given chi Router.
func MountRoutes(
	r chi.Router,
	authHandler *auth.Handler,
	ctrlHandler *Handler,
	tokenManager *auth.TokenManager,
) {
	const maxAdminBodyBytes = 5 << 20 // 5 MB max body size for administrative requests
	limitBody := maxBodySizeMiddleware(maxAdminBodyBytes)

	// Health probe (unauthenticated)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"control_plane": "ready",
		})
	})

	if authHandler != nil {
		r.With(limitBody).Post("/auth/login", authHandler.HandleLogin)
		r.Post("/auth/logout", authHandler.HandleLogout)
	}

	if tokenManager == nil || ctrlHandler == nil {
		return
	}

	authMw := auth.Middleware(tokenManager)
	requireViewer := auth.RequireRole(auth.RoleViewer)
	requireEditor := auth.RequireRole(auth.RoleEditor)
	requireOwner := auth.RequireRole(auth.RoleOwner)

	// Protected routes (require valid admin session)
	r.Group(func(admin chi.Router) {
		admin.Use(limitBody)
		admin.Use(authMw)

		// Current Admin Profile
		if authHandler != nil {
			admin.With(requireViewer).Get("/auth/me", authHandler.HandleMe)
			admin.With(requireOwner).Get("/users", authHandler.HandleListUsers)
			admin.With(requireOwner).Post("/users", authHandler.HandleCreateUser)
			admin.With(requireOwner).Delete("/users/{id}", authHandler.HandleDeleteUser)
		}

		// Projects & Environments
		admin.With(requireViewer).Get("/projects", ctrlHandler.HandleListProjects)
		admin.With(requireOwner).Post("/projects", ctrlHandler.HandleCreateProject)
		admin.With(requireViewer).Get("/projects/{project_id}", ctrlHandler.HandleGetProject)
		admin.With(requireOwner).Put("/projects/{project_id}", ctrlHandler.HandleUpdateProject)
		admin.With(requireOwner).Delete("/projects/{project_id}", ctrlHandler.HandleDeleteProject)
		admin.With(requireViewer).Get("/projects/{project_id}/environments", ctrlHandler.HandleListEnvironments)
		admin.With(requireOwner).Post("/projects/{project_id}/environments", ctrlHandler.HandleCreateEnvironment)
		admin.With(requireOwner).Delete("/projects/{project_id}/environments/{name}", ctrlHandler.HandleDeleteEnvironment)
		admin.With(requireViewer).Get("/projects/{project_id}/environments/{name}/auth", ctrlHandler.HandleGetEnvironmentAuthConfig)
		admin.With(requireEditor).Put("/projects/{project_id}/environments/{name}/auth", ctrlHandler.HandleUpdateEnvironmentAuthConfig)
		admin.With(requireViewer).Get("/projects/{project_id}/environments/{name}/export", ctrlHandler.HandleExportConfig)
		admin.With(requireEditor).Post("/projects/{project_id}/environments/{name}/import", ctrlHandler.HandleImportConfig)
		admin.With(requireViewer).Post("/auth/test-token", ctrlHandler.HandleTestToken)

		// AI Apps
		admin.With(requireViewer).Get("/apps", ctrlHandler.HandleListAIApps)
		admin.With(requireEditor).Post("/apps", ctrlHandler.HandleCreateAIApp)
		admin.With(requireViewer).Get("/apps/{id}", ctrlHandler.HandleGetAIApp)
		admin.With(requireEditor).Put("/apps/{id}", ctrlHandler.HandleUpdateAIApp)
		admin.With(requireEditor).Delete("/apps/{id}", ctrlHandler.HandleDeleteAIApp)
		admin.With(requireViewer).Get("/apps/{id}/keys", ctrlHandler.HandleListAppAPIKeys)
		admin.With(requireEditor).Post("/apps/{id}/keys", ctrlHandler.HandleCreateAppAPIKey)
		admin.With(requireEditor).Delete("/apps/{id}/keys/{key_id}", ctrlHandler.HandleDeleteAppAPIKey)

		// Prompts
		admin.With(requireViewer).Get("/prompts", ctrlHandler.HandleListPrompts)
		admin.With(requireEditor).Post("/prompts", ctrlHandler.HandleCreatePrompt)
		admin.With(requireViewer).Get("/prompts/{id}", ctrlHandler.HandleGetPrompt)
		admin.With(requireEditor).Put("/prompts/{id}", ctrlHandler.HandleUpdatePrompt)
		admin.With(requireEditor).Post("/prompts/{id}/versions", ctrlHandler.HandleCreatePromptVersion)
		admin.With(requireEditor).Put("/prompts/{id}/active-version", ctrlHandler.HandleSetActivePromptVersion)
		admin.With(requireEditor).Delete("/prompts/{id}", ctrlHandler.HandleDeletePrompt)

		// Skills
		admin.With(requireViewer).Get("/skills", ctrlHandler.HandleListSkills)
		admin.With(requireEditor).Post("/skills", ctrlHandler.HandleCreateSkill)
		admin.With(requireEditor).Post("/skills/import", ctrlHandler.HandleImportSkill)
		admin.With(requireViewer).Get("/skills/{id}", ctrlHandler.HandleGetSkill)
		admin.With(requireEditor).Put("/skills/{id}", ctrlHandler.HandleUpdateSkill)
		admin.With(requireEditor).Post("/skills/{id}/versions", ctrlHandler.HandleCreateSkillVersion)
		admin.With(requireEditor).Put("/skills/{id}/active-version", ctrlHandler.HandleSetActiveSkillVersion)
		admin.With(requireEditor).Delete("/skills/{id}", ctrlHandler.HandleDeleteSkill)

		// Model Routings & Providers
		admin.With(requireViewer).Get("/models", ctrlHandler.HandleListModelRoutings)
		admin.With(requireEditor).Put("/models/{logical_model}", ctrlHandler.HandleUpsertModelRouting)
		admin.With(requireEditor).Delete("/models/{logical_model}", ctrlHandler.HandleDeleteModelRouting)
		admin.With(requireViewer).Get("/providers", ctrlHandler.HandleListProviderConfigs)
		admin.With(requireViewer).Get("/providers/{provider_name}/models", ctrlHandler.HandleFetchProviderModels)
		admin.With(requireViewer).Post("/providers/test-connection", ctrlHandler.HandleTestProviderConfig)
		admin.With(requireOwner).Put("/providers/{provider_name}", ctrlHandler.HandleUpsertProviderConfig)
		admin.With(requireOwner).Delete("/providers/{provider_name}", ctrlHandler.HandleDeleteProviderConfig)

		// Tools
		admin.With(requireViewer).Get("/tools", ctrlHandler.HandleListTools)
		admin.With(requireEditor).Post("/tools", ctrlHandler.HandleCreateTool)
		admin.With(requireViewer).Get("/tools/{id}", ctrlHandler.HandleGetTool)
		admin.With(requireEditor).Put("/tools/{id}", ctrlHandler.HandleUpdateTool)
		admin.With(requireEditor).Put("/tools/{id}/active-version", ctrlHandler.HandleSetActiveToolVersion)
		admin.With(requireEditor).Delete("/tools/{id}", ctrlHandler.HandleDeleteTool)
		admin.With(requireEditor).Put("/tools/{id}/secrets/{secret_ref}", ctrlHandler.HandleUpsertToolSecret)
		admin.With(requireViewer).Post("/tools/{id}/test", ctrlHandler.HandleTestTool)

		// Search Engine (SearXNG)
		admin.With(requireViewer).Get("/search/status", ctrlHandler.HandleGetSearchStatus)
		admin.With(requireViewer).Post("/search/test", ctrlHandler.HandleTestSearch)
		admin.With(requireEditor).Put("/search/config", ctrlHandler.HandleUpdateSearchConfig)

		// Policies
		admin.With(requireViewer).Get("/policies", ctrlHandler.HandleListPolicies)
		admin.With(requireEditor).Post("/policies", ctrlHandler.HandleCreatePolicy)
		admin.With(requireViewer).Get("/policies/{id}", ctrlHandler.HandleGetPolicy)
		admin.With(requireEditor).Post("/policies/{id}/versions", ctrlHandler.HandleCreatePolicyVersion)
		admin.With(requireEditor).Put("/policies/{id}/active-version", ctrlHandler.HandleSetActivePolicyVersion)
		admin.With(requireEditor).Delete("/policies/{id}", ctrlHandler.HandleDeletePolicy)
		admin.With(requireViewer).Post("/policies/test", ctrlHandler.HandleTestPolicy)

		// Observability, Logs & Privacy
		admin.With(requireViewer).Get("/usage", ctrlHandler.HandleGetUsage)
		admin.With(requireViewer).Get("/logs", ctrlHandler.HandleGetLogs)
		admin.With(requireViewer).Get("/logs/{request_id}", ctrlHandler.HandleGetLogByID)
		admin.With(requireViewer).Get("/audit-logs", ctrlHandler.HandleListAuditLogs)
		admin.With(requireOwner).Delete("/subjects/{issuer}/{subject}", ctrlHandler.HandlePrivacyPurgeSubject)
	})
}
