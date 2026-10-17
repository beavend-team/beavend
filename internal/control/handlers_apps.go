package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	SkillIDs       json.RawMessage `json:"skill_ids"`
	Limits         json.RawMessage `json:"limits"`
	CacheConfig    json.RawMessage `json:"cache_config"`
	Guardrails     json.RawMessage `json:"guardrails"`
	ResponseSchema string          `json:"response_schema"`
	MemoryConfig   json.RawMessage `json:"memory_config"`
	Memory         json.RawMessage `json:"memory"`
	SearchConfig   json.RawMessage `json:"search_config"`
	Search         json.RawMessage `json:"search"`
	Auth           json.RawMessage `json:"auth"`
	AuthConfig     json.RawMessage `json:"auth_config"`
	ModelRoutes    json.RawMessage `json:"model_routes"`
}

type AIAppResponse struct {
	ID            string             `json:"id"`
	ProjectID     string             `json:"project_id"`
	Environment   string             `json:"environment"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	ActiveVersion int32              `json:"active_version"`
	CreatedAt     pgtype.Timestamptz `json:"created_at"`
	UpdatedAt     pgtype.Timestamptz `json:"updated_at"`
	AuthConfig    json.RawMessage    `json:"auth_config,omitempty"`
	Auth          json.RawMessage    `json:"auth,omitempty"`
	ModelRoutes   json.RawMessage    `json:"model_routes,omitempty"`
}

type AIAppVersionResponse struct {
	ID             string             `json:"id"`
	AppID          string             `json:"app_id"`
	ProjectID      string             `json:"project_id"`
	Environment    string             `json:"environment"`
	Version        int32              `json:"version"`
	AccessPolicy   string             `json:"access_policy"`
	PromptID       string             `json:"prompt_id"`
	PromptVersion  int32              `json:"prompt_version"`
	ModelRoutingID string             `json:"model_routing_id"`
	ToolIds        json.RawMessage    `json:"tool_ids,omitempty"`
	SkillIds       json.RawMessage    `json:"skill_ids,omitempty"`
	Limits         json.RawMessage    `json:"limits,omitempty"`
	CacheConfig    json.RawMessage    `json:"cache_config,omitempty"`
	Guardrails     json.RawMessage    `json:"guardrails,omitempty"`
	ResponseSchema string             `json:"response_schema"`
	IsActive       bool               `json:"is_active"`
	CreatedAt      pgtype.Timestamptz `json:"created_at"`
	MemoryConfig   json.RawMessage    `json:"memory_config,omitempty"`
	SearchConfig   json.RawMessage    `json:"search_config,omitempty"`
}

type AIAppDetailsResponse struct {
	App      AIAppResponse          `json:"app"`
	Versions []AIAppVersionResponse `json:"versions"`
}

func toAIAppResponse(app db.AiApps) AIAppResponse {
	var auth json.RawMessage
	if len(app.AuthConfig) > 0 && string(app.AuthConfig) != "null" && string(app.AuthConfig) != "{}" {
		auth = json.RawMessage(app.AuthConfig)
	}
	var routes json.RawMessage
	if len(app.ModelRoutes) > 0 && string(app.ModelRoutes) != "null" && string(app.ModelRoutes) != "{}" {
		routes = json.RawMessage(app.ModelRoutes)
	}
	return AIAppResponse{
		ID:            app.ID,
		ProjectID:     app.ProjectID,
		Environment:   app.Environment,
		Name:          app.Name,
		Description:   app.Description,
		ActiveVersion: app.ActiveVersion,
		CreatedAt:     app.CreatedAt,
		UpdatedAt:     app.UpdatedAt,
		AuthConfig:    auth,
		Auth:          auth,
		ModelRoutes:   routes,
	}
}

func toAIAppVersionResponse(v db.AiAppVersions) AIAppVersionResponse {
	raw := func(b []byte) json.RawMessage {
		if len(b) > 0 && string(b) != "null" && string(b) != "{}" && string(b) != "[]" {
			return json.RawMessage(b)
		}
		return nil
	}
	return AIAppVersionResponse{
		ID:             v.ID,
		AppID:          v.AppID,
		ProjectID:      v.ProjectID,
		Environment:    v.Environment,
		Version:        v.Version,
		AccessPolicy:   v.AccessPolicy,
		PromptID:       v.PromptID,
		PromptVersion:  v.PromptVersion,
		ModelRoutingID: v.ModelRoutingID,
		ToolIds:        raw(v.ToolIds),
		SkillIds:       raw(v.SkillIds),
		Limits:         raw(v.Limits),
		CacheConfig:    raw(v.CacheConfig),
		Guardrails:     raw(v.Guardrails),
		ResponseSchema: v.ResponseSchema,
		IsActive:       v.IsActive,
		CreatedAt:      v.CreatedAt,
		MemoryConfig:   raw(v.MemoryConfig),
		SearchConfig:   raw(v.SearchConfig),
	}
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

	resp := make([]AIAppResponse, len(apps))
	for i, a := range apps {
		resp[i] = toAIAppResponse(a)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleCreateAIApp(w http.ResponseWriter, r *http.Request) {
	projectID, env := getProjectAndEnv(r)
	var req CreateAIAppRequest
	if !decodeJSON(w, r, &req) {
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
	if len(req.SkillIDs) == 0 || string(req.SkillIDs) == "null" {
		req.SkillIDs = []byte("[]")
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
	memBytes := req.MemoryConfig
	if len(memBytes) == 0 {
		memBytes = req.Memory
	}
	if len(memBytes) == 0 {
		memBytes = []byte("{}")
	}

	searchBytes := req.SearchConfig
	if len(searchBytes) == 0 {
		searchBytes = req.Search
	}
	if len(searchBytes) == 0 {
		searchBytes = []byte("{}")
	}

	authBytes := req.AuthConfig
	if len(authBytes) == 0 {
		authBytes = req.Auth
	}
	if len(authBytes) == 0 || string(authBytes) == "null" {
		authBytes = nil
	} else {
		var ac core.AuthConfig
		if err := json.Unmarshal(authBytes, &ac); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("invalid auth config: %v", err))
			return
		}
		if ac.Provider != "" {
			validProviders := map[string]bool{
				"oidc": true, "firebase": true, "supabase": true, "jwt": true, "anonymous": true,
			}
			if !validProviders[strings.ToLower(ac.Provider)] {
				writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("unsupported auth provider %q; must be one of: oidc, firebase, supabase, jwt, anonymous", ac.Provider))
				return
			}
		}
	}

	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}

	if err := validateModelRoutesShape(req.ModelRoutes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}

	routesBytes := req.ModelRoutes
	if len(routesBytes) == 0 || string(routesBytes) == "null" {
		if req.ModelRoutingID != "" {
			routesBytes, _ = json.Marshal(core.ModelRoutes{Default: req.ModelRoutingID})
		} else {
			routesBytes = nil
		}
	}

	app, err := h.svc.queries.CreateAIApp(r.Context(), db.CreateAIAppParams{
		ID:            req.ID,
		ProjectID:     projectID,
		Environment:   env,
		Name:          req.Name,
		Description:   req.Description,
		ActiveVersion: 1,
		AuthConfig:    authBytes,
		ModelRoutes:   routesBytes,
	})
	if err != nil {
		if strings.Contains(err.Error(), "ai_apps_project_id_environment_fkey") || strings.Contains(err.Error(), "violates foreign key constraint") {
			writeError(w, http.StatusBadRequest, "project_not_found", fmt.Sprintf("project %q with environment %q does not exist. Please select or create a valid project first.", projectID, env))
			return
		}
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
		SkillIds:       req.SkillIDs,
		Limits:         req.Limits,
		CacheConfig:    req.CacheConfig,
		Guardrails:     req.Guardrails,
		ResponseSchema: req.ResponseSchema,
		IsActive:       true,
		MemoryConfig:   memBytes,
		SearchConfig:   searchBytes,
	})
	if err != nil {
		_ = h.svc.queries.DeleteAIApp(r.Context(), req.ID)
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_app", "ai_app", app.ID, req)

	writeJSON(w, http.StatusCreated, toAIAppResponse(app))
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

	respVersions := make([]AIAppVersionResponse, len(versions))
	for i, v := range versions {
		respVersions[i] = toAIAppVersionResponse(v)
	}

	writeJSON(w, http.StatusOK, AIAppDetailsResponse{
		App:      toAIAppResponse(app),
		Versions: respVersions,
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
	if !decodeJSON(w, r, &req) {
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
	skillBytes := req.SkillIDs
	if len(skillBytes) == 0 || string(skillBytes) == "null" {
		prevVer, err := h.svc.queries.GetAIAppVersion(r.Context(), db.GetAIAppVersionParams{
			AppID:   appID,
			Version: existing.ActiveVersion,
		})
		if err == nil && len(prevVer.SkillIds) > 0 && string(prevVer.SkillIds) != "null" {
			skillBytes = prevVer.SkillIds
		} else {
			skillBytes = []byte("[]")
		}
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
	memBytes := req.MemoryConfig
	if len(memBytes) == 0 {
		memBytes = req.Memory
	}
	if len(memBytes) == 0 {
		memBytes = []byte("{}")
	}

	searchBytes := req.SearchConfig
	if len(searchBytes) == 0 {
		searchBytes = req.Search
	}
	if len(searchBytes) == 0 {
		searchBytes = []byte("{}")
	}

	authBytes := req.AuthConfig
	if len(authBytes) == 0 {
		authBytes = req.Auth
	}
	if len(authBytes) == 0 {
		authBytes = existing.AuthConfig
	} else if string(authBytes) == "null" {
		authBytes = nil
	} else {
		var ac core.AuthConfig
		if err := json.Unmarshal(authBytes, &ac); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("invalid auth config: %v", err))
			return
		}
		if ac.Provider != "" {
			validProviders := map[string]bool{
				"oidc": true, "firebase": true, "supabase": true, "jwt": true, "anonymous": true,
			}
			if !validProviders[strings.ToLower(ac.Provider)] {
				writeError(w, http.StatusBadRequest, "invalid_argument", fmt.Sprintf("unsupported auth provider %q; must be one of: oidc, firebase, supabase, jwt, anonymous", ac.Provider))
				return
			}
		}
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
		SkillIds:       skillBytes,
		Limits:         req.Limits,
		CacheConfig:    req.CacheConfig,
		Guardrails:     req.Guardrails,
		ResponseSchema: req.ResponseSchema,
		IsActive:       true,
		MemoryConfig:   memBytes,
		SearchConfig:   searchBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	appName := strings.TrimSpace(req.Name)
	if appName == "" {
		appName = existing.Name
	}
	appDesc := req.Description
	if appDesc == "" {
		appDesc = existing.Description
	}

	if err := validateModelRoutesShape(req.ModelRoutes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}

	routesBytes := req.ModelRoutes
	if len(routesBytes) == 0 || string(routesBytes) == "null" {
		if req.ModelRoutingID != "" {
			routesBytes, _ = json.Marshal(core.ModelRoutes{Default: req.ModelRoutingID})
		} else {
			routesBytes = existing.ModelRoutes
		}
	}

	_, _ = h.svc.queries.UpdateAIApp(r.Context(), db.UpdateAIAppParams{
		ID:          appID,
		Name:        appName,
		Description: appDesc,
		AuthConfig:  authBytes,
		ModelRoutes: routesBytes,
	})

	updatedApp, err := h.svc.queries.UpdateAIAppActiveVersion(r.Context(), db.UpdateAIAppActiveVersionParams{
		ID:            appID,
		ActiveVersion: newVersionNum,
		AuthConfig:    authBytes,
		ModelRoutes:   routesBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	updatedApp.Name = appName
	updatedApp.Description = appDesc

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "update_app", "ai_app", appID, req)

	writeJSON(w, http.StatusOK, toAIAppResponse(updatedApp))
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
