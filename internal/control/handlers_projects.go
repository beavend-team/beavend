package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/store/db"
	"github.com/go-chi/chi/v5"
)

// Handler provides HTTP endpoints for administrative control plane operations.
type Handler struct {
	svc *Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// --- Projects ---

type CreateProjectRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *Handler) HandleListProjects(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}

	projects, err := h.svc.queries.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, projects)
}

func (h *Handler) HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id and name are required")
		return
	}

	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}

	proj, err := h.svc.queries.CreateProject(r.Context(), db.CreateProjectParams{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}

	// Create default environments: development, staging, production
	defaultEnvs := []string{"development", "staging", "production"}
	for _, env := range defaultEnvs {
		_, _ = h.svc.queries.CreateEnvironment(r.Context(), db.CreateEnvironmentParams{
			ProjectID:   proj.ID,
			Name:        env,
			Description: env + " environment",
		})
	}

	h.svc.RecordAudit(r.Context(), proj.ID, "development", "create_project", "project", proj.ID, req)
	writeJSON(w, http.StatusCreated, proj)
}

func (h *Handler) HandleGetProject(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "project id required")
		return
	}

	proj, err := h.svc.queries.GetProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	writeJSON(w, http.StatusOK, proj)
}

func (h *Handler) HandleUpdateProject(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")
	var req CreateProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	proj, err := h.svc.queries.UpdateProject(r.Context(), db.UpdateProjectParams{
		ID:          projectID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	h.svc.RecordAudit(r.Context(), projectID, "development", "update_project", "project", projectID, req)
	writeJSON(w, http.StatusOK, proj)
}

func (h *Handler) HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")

	if err := h.svc.queries.DeleteProject(r.Context(), projectID); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	h.svc.RecordAudit(r.Context(), projectID, "development", "delete_project", "project", projectID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": projectID})
}

// --- Environments ---

type CreateEnvironmentRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *Handler) HandleListEnvironments(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")
	envs, err := h.svc.queries.ListEnvironments(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, envs)
}

func (h *Handler) HandleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")
	var req CreateEnvironmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}

	env, err := h.svc.queries.CreateEnvironment(r.Context(), db.CreateEnvironmentParams{
		ProjectID:   projectID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}

	h.svc.RecordAudit(r.Context(), projectID, req.Name, "create_environment", "environment", req.Name, req)
	writeJSON(w, http.StatusCreated, env)
}

func (h *Handler) HandleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")
	name := chi.URLParam(r, "name")

	if err := h.svc.queries.DeleteEnvironment(r.Context(), db.DeleteEnvironmentParams{
		ProjectID: projectID,
		Name:      name,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	h.svc.RecordAudit(r.Context(), projectID, name, "delete_environment", "environment", name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

// --- Environment AuthConfig & JWT Token Testing ---

type TestTokenRequest struct {
	Token      string           `json:"token"`
	AuthConfig *core.AuthConfig `json:"auth_config,omitempty"`
}

type TestTokenResponse struct {
	Valid     bool           `json:"valid"`
	Error     string         `json:"error,omitempty"`
	Subject   string         `json:"subject,omitempty"`
	Issuer    string         `json:"issuer,omitempty"`
	Audience  string         `json:"audience,omitempty"`
	IssuedAt  string         `json:"issued_at,omitempty"`
	ExpiresAt string         `json:"expires_at,omitempty"`
	Claims    map[string]any `json:"claims,omitempty"`
}

func (h *Handler) HandleGetEnvironmentAuthConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")
	name := chi.URLParam(r, "name")

	env, err := h.svc.queries.GetEnvironment(r.Context(), db.GetEnvironmentParams{
		ProjectID: projectID,
		Name:      name,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "environment not found")
		return
	}

	if len(env.AuthConfig) == 0 || string(env.AuthConfig) == "{}" || string(env.AuthConfig) == "null" {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(env.AuthConfig)
}

func (h *Handler) HandleUpdateEnvironmentAuthConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID := chi.URLParam(r, "project_id")
	name := chi.URLParam(r, "name")

	var cfg core.AuthConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid auth config payload")
		return
	}

	updated, err := h.svc.queries.UpdateEnvironmentAuthConfig(r.Context(), db.UpdateEnvironmentAuthConfigParams{
		ProjectID:  projectID,
		Name:       name,
		AuthConfig: data,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, name)
	h.svc.RecordAudit(r.Context(), projectID, name, "update_auth_config", "environment", name, cfg)

	if len(updated.AuthConfig) == 0 || string(updated.AuthConfig) == "{}" {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(updated.AuthConfig)
}

func (h *Handler) HandleTestToken(w http.ResponseWriter, r *http.Request) {
	var req TestTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	tokenStr := strings.TrimSpace(req.Token)
	if tokenStr == "" {
		writeJSON(w, http.StatusOK, TestTokenResponse{
			Valid: false,
			Error: "token is required",
		})
		return
	}

	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	tokenStr = strings.TrimSpace(tokenStr)

	authCfg := req.AuthConfig
	if authCfg == nil || (authCfg.Provider == "" && authCfg.Issuer == "" && authCfg.JWKSURL == "") {
		projectID, env := getProjectAndEnv(r)
		if h.svc.queries != nil {
			envRow, err := h.svc.queries.GetEnvironment(r.Context(), db.GetEnvironmentParams{
				ProjectID: projectID,
				Name:      env,
			})
			if err == nil && len(envRow.AuthConfig) > 0 && string(envRow.AuthConfig) != "{}" {
				var parsed core.AuthConfig
				if err := json.Unmarshal(envRow.AuthConfig, &parsed); err == nil {
					authCfg = &parsed
				}
			}
		}
	}

	if authCfg == nil {
		writeJSON(w, http.StatusOK, TestTokenResponse{
			Valid: false,
			Error: "no auth configuration provided or configured for this environment",
		})
		return
	}

	verifier := identity.NewVerifier(identity.NewJWKSManager())
	jwtToken, err := verifier.Verify(r.Context(), tokenStr, authCfg)
	if err != nil {
		writeJSON(w, http.StatusOK, TestTokenResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	projectID, env := getProjectAndEnv(r)
	normalizer := identity.NewNormalizer(authCfg.Provider)
	id, err := normalizer.Normalize(r.Context(), jwtToken, projectID, env)
	if err != nil {
		writeJSON(w, http.StatusOK, TestTokenResponse{
			Valid: false,
			Error: fmt.Sprintf("normalization error: %v", err),
		})
		return
	}

	var expiresAt string
	if !jwtToken.Expiration().IsZero() {
		expiresAt = jwtToken.Expiration().UTC().Format(time.RFC3339)
	}
	var issuedAt string
	if !jwtToken.IssuedAt().IsZero() {
		issuedAt = jwtToken.IssuedAt().UTC().Format(time.RFC3339)
	}

	writeJSON(w, http.StatusOK, TestTokenResponse{
		Valid:     true,
		Subject:   id.Subject,
		Issuer:    id.Issuer,
		Audience:  id.Audience,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
		Claims:    id.Claims,
	})
}
