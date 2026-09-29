package control

import (
	"encoding/json"
	"net/http"
	"strings"

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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
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
	projectID := chi.URLParam(r, "project_id")
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
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
	projectID := chi.URLParam(r, "project_id")
	envs, err := h.svc.queries.ListEnvironments(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, envs)
}

func (h *Handler) HandleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "project_id")
	var req CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
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
