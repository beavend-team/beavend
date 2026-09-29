package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/store/db"
	"github.com/go-chi/chi/v5"
)

type CreateAIAppRequest struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	AccessPolicy   string          `json:"access_policy"`
	PromptID       string          `json:"prompt_id"`
	PromptVersion  int32           `json:"prompt_version"`
	ModelRoutingID string          `json:"model_routing_id"`
	ToolIDs        json.RawMessage `json:"tool_ids"`
	Limits         json.RawMessage `json:"limits"`
	CacheConfig    json.RawMessage `json:"cache_config"`
	Guardrails     json.RawMessage `json:"guardrails"`
	ResponseSchema string          `json:"response_schema"`
}

type AIAppDetailsResponse struct {
	App      db.AiApps          `json:"app"`
	Versions []db.AiAppVersions `json:"versions"`
}

func (h *Handler) HandleListAIApps(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	apps, err := h.svc.queries.ListAIApps(r.Context(), db.ListAIAppsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, apps)
}

func (h *Handler) HandleCreateAIApp(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	var req CreateAIAppRequest
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

	if req.PromptVersion <= 0 {
		req.PromptVersion = 1
	}
	if len(req.ToolIDs) == 0 {
		req.ToolIDs = []byte("[]")
	}
	if len(req.Limits) == 0 {
		req.Limits = []byte("[]")
	}
	if len(req.CacheConfig) == 0 {
		req.CacheConfig = []byte("{}")
	}
	if len(req.Guardrails) == 0 {
		req.Guardrails = []byte("{}")
	}

	app, err := h.svc.queries.CreateAIApp(r.Context(), db.CreateAIAppParams{
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
	_, err = h.svc.queries.CreateAIAppVersion(r.Context(), db.CreateAIAppVersionParams{
		ID:             versionID,
		AppID:          req.ID,
		ProjectID:      projectID,
		Environment:    env,
		Version:        1,
		AccessPolicy:   req.AccessPolicy,
		PromptID:       req.PromptID,
		PromptVersion:  req.PromptVersion,
		ModelRoutingID: req.ModelRoutingID,
		ToolIds:        req.ToolIDs,
		Limits:         req.Limits,
		CacheConfig:    req.CacheConfig,
		Guardrails:     req.Guardrails,
		ResponseSchema: req.ResponseSchema,
		IsActive:       true,
	})
	if err != nil {
		_ = h.svc.queries.DeleteAIApp(r.Context(), req.ID)
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_app", "ai_app", app.ID, req)

	writeJSON(w, http.StatusCreated, app)
}

func (h *Handler) HandleGetAIApp(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	appID := chi.URLParam(r, "id")
	app, err := h.svc.queries.GetAIApp(r.Context(), appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "ai app not found")
		return
	}

	versions, err := h.svc.queries.ListAIAppVersions(r.Context(), appID)
	if err != nil {
		versions = []db.AiAppVersions{}
	}

	writeJSON(w, http.StatusOK, AIAppDetailsResponse{
		App:      app,
		Versions: versions,
	})
}

func (h *Handler) HandleUpdateAIApp(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	appID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req CreateAIAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	existing, err := h.svc.queries.GetAIApp(r.Context(), appID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "ai app not found")
		return
	}

	newVersionNum := existing.ActiveVersion + 1
	versionID := fmt.Sprintf("%s_v%d", appID, newVersionNum)

	if req.PromptVersion <= 0 {
		req.PromptVersion = 1
	}
	if len(req.ToolIDs) == 0 {
		req.ToolIDs = []byte("[]")
	}
	if len(req.Limits) == 0 {
		req.Limits = []byte("[]")
	}
	if len(req.CacheConfig) == 0 {
		req.CacheConfig = []byte("{}")
	}
	if len(req.Guardrails) == 0 {
		req.Guardrails = []byte("{}")
	}

	_, err = h.svc.queries.CreateAIAppVersion(r.Context(), db.CreateAIAppVersionParams{
		ID:             versionID,
		AppID:          appID,
		ProjectID:      projectID,
		Environment:    env,
		Version:        newVersionNum,
		AccessPolicy:   req.AccessPolicy,
		PromptID:       req.PromptID,
		PromptVersion:  req.PromptVersion,
		ModelRoutingID: req.ModelRoutingID,
		ToolIds:        req.ToolIDs,
		Limits:         req.Limits,
		CacheConfig:    req.CacheConfig,
		Guardrails:     req.Guardrails,
		ResponseSchema: req.ResponseSchema,
		IsActive:       true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	updatedApp, err := h.svc.queries.UpdateAIAppActiveVersion(r.Context(), db.UpdateAIAppActiveVersionParams{
		ID:            appID,
		ActiveVersion: newVersionNum,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "update_app", "ai_app", appID, req)

	writeJSON(w, http.StatusOK, updatedApp)
}

func (h *Handler) HandleDeleteAIApp(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	appID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	if err := h.svc.queries.DeleteAIApp(r.Context(), appID); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_app", "ai_app", appID, nil)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": appID})
}
