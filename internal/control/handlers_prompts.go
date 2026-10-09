package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
)

type CreatePromptRequest struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Template    string          `json:"template"`
	Variables   json.RawMessage `json:"variables"`
}

type CreatePromptVersionRequest struct {
	Template  string          `json:"template"`
	Variables json.RawMessage `json:"variables"`
	SetActive bool            `json:"set_active"`
}

type UpdatePromptRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Template    string          `json:"template"`
	Variables   json.RawMessage `json:"variables"`
}

type PromptDetailsResponse struct {
	Prompt        db.Prompts         `json:"prompt"`
	ActiveVersion *db.PromptVersions `json:"active_version,omitempty"`
	Versions      []db.PromptVersions `json:"versions"`
}

func (h *Handler) HandleListPrompts(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	prompts, err := h.svc.queries.ListPrompts(r.Context(), db.ListPromptsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, prompts)
}

func (h *Handler) HandleCreatePrompt(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	var req CreatePromptRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id and name are required")
		return
	}
	if len(req.Variables) == 0 {
		req.Variables = []byte("[]")
	}

	prompt, err := h.svc.queries.CreatePrompt(r.Context(), db.CreatePromptParams{
		ID:            req.ID,
		ProjectID:     projectID,
		Environment:   env,
		Name:          req.Name,
		Description:   req.Description,
		ActiveVersion: 1,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}

	versionID := fmt.Sprintf("%s_v1", req.ID)
	_, err = h.svc.queries.CreatePromptVersion(r.Context(), db.CreatePromptVersionParams{
		ID:          versionID,
		PromptID:    req.ID,
		ProjectID:   projectID,
		Environment: env,
		Version:     1,
		Template:    req.Template,
		Variables:   req.Variables,
		IsActive:    true,
	})
	if err != nil {
		_ = h.svc.queries.DeletePrompt(r.Context(), req.ID)
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_prompt", "prompt", prompt.ID, req)

	writeJSON(w, http.StatusCreated, prompt)
}

func (h *Handler) HandleGetPrompt(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	promptID := chi.URLParam(r, "id")
	prompt, err := h.svc.queries.GetPrompt(r.Context(), promptID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "prompt not found")
		return
	}

	versions, err := h.svc.queries.ListPromptVersions(r.Context(), promptID)
	if err != nil {
		versions = []db.PromptVersions{}
	}

	var activePtr *db.PromptVersions
	activeVer, err := h.svc.queries.GetActivePromptVersion(r.Context(), promptID)
	if err == nil {
		activePtr = &activeVer
	} else if len(versions) > 0 {
		activePtr = &versions[0]
	}

	writeJSON(w, http.StatusOK, PromptDetailsResponse{
		Prompt:        prompt,
		ActiveVersion: activePtr,
		Versions:      versions,
	})
}

func (h *Handler) HandleUpdatePrompt(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	promptID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req UpdatePromptRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := h.svc.queries.GetPrompt(r.Context(), promptID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "prompt not found")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = existing.Name
	}
	desc := req.Description
	if desc == "" {
		desc = existing.Description
	}

	activeVer := existing.ActiveVersion
	if strings.TrimSpace(req.Template) != "" {
		newVersion := existing.ActiveVersion + 1
		versionID := fmt.Sprintf("%s_v%d", promptID, newVersion)
		if len(req.Variables) == 0 {
			req.Variables = []byte("[]")
		}

		_, err = h.svc.queries.CreatePromptVersion(r.Context(), db.CreatePromptVersionParams{
			ID:          versionID,
			PromptID:    promptID,
			ProjectID:   projectID,
			Environment: env,
			Version:     newVersion,
			Template:    req.Template,
			Variables:   req.Variables,
			IsActive:    true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database_error", err.Error())
			return
		}
		activeVer = newVersion
	}

	updatedPrompt, err := h.svc.queries.UpdatePrompt(r.Context(), db.UpdatePromptParams{
		ID:            promptID,
		Name:          name,
		Description:   desc,
		ActiveVersion: activeVer,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "update_prompt", "prompt", promptID, req)

	writeJSON(w, http.StatusOK, updatedPrompt)
}

func (h *Handler) HandleCreatePromptVersion(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	promptID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req CreatePromptVersionRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := h.svc.queries.GetPrompt(r.Context(), promptID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "prompt not found")
		return
	}

	newVersion := existing.ActiveVersion + 1
	versionID := fmt.Sprintf("%s_v%d", promptID, newVersion)
	if len(req.Variables) == 0 {
		req.Variables = []byte("[]")
	}

	createdVersion, err := h.svc.queries.CreatePromptVersion(r.Context(), db.CreatePromptVersionParams{
		ID:          versionID,
		PromptID:    promptID,
		ProjectID:   projectID,
		Environment: env,
		Version:     newVersion,
		Template:    req.Template,
		Variables:   req.Variables,
		IsActive:    req.SetActive,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	if req.SetActive {
		_, _ = h.svc.queries.UpdatePromptActiveVersion(r.Context(), db.UpdatePromptActiveVersionParams{
			ID:            promptID,
			ActiveVersion: newVersion,
		})
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_prompt_version", "prompt", promptID, req)

	writeJSON(w, http.StatusCreated, createdVersion)
}

func (h *Handler) HandleDeletePrompt(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	promptID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	if err := h.svc.queries.DeletePrompt(r.Context(), promptID); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_prompt", "prompt", promptID, nil)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": promptID})
}

func (h *Handler) HandleSetActivePromptVersion(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	promptID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req SetActiveVersionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "valid version is required")
		return
	}

	_, err := h.svc.queries.GetPromptVersion(r.Context(), db.GetPromptVersionParams{
		PromptID: promptID,
		Version:  req.Version,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "prompt version not found")
		return
	}

	updated, err := h.svc.queries.UpdatePromptActiveVersion(r.Context(), db.UpdatePromptActiveVersionParams{
		ID:            promptID,
		ActiveVersion: req.Version,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "set_active_prompt_version", "prompt", promptID, req)

	writeJSON(w, http.StatusOK, updated)
}
