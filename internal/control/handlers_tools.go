package control

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/secrets"
	"github.com/beavend/beavend/internal/store/db"
	"github.com/go-chi/chi/v5"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

type CreateToolRequest struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	SideEffect       string          `json:"side_effect"`
	InputSchema      json.RawMessage `json:"input_schema"`
	IdentityBindings json.RawMessage `json:"identity_bindings"`
	Executor         json.RawMessage `json:"executor"`
	DownstreamAuth   json.RawMessage `json:"downstream_auth"`
	Confirmation     string          `json:"confirmation"`
	Authorization    string          `json:"authorization"`
	ResultConfig     json.RawMessage `json:"result_config"`
	CacheConfig      json.RawMessage `json:"cache_config"`
}

type ToolDetailsResponse struct {
	Tool     db.Tools          `json:"tool"`
	Versions []db.ToolVersions `json:"versions"`
}

func (h *Handler) HandleListTools(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	tools, err := h.svc.queries.ListTools(r.Context(), db.ListToolsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tools)
}

func (h *Handler) HandleCreateTool(w http.ResponseWriter, r *http.Request) {
	projectID, env := getProjectAndEnv(r)
	var req CreateToolRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id and name are required")
		return
	}

	if req.SideEffect == "" {
		req.SideEffect = "read"
	}
	if req.Confirmation == "" {
		req.Confirmation = "none"
	}
	if len(req.InputSchema) == 0 {
		req.InputSchema = []byte("{}")
	} else {
		// Validate JSON schema syntax
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("schema.json", strings.NewReader(string(req.InputSchema))); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schema", fmt.Sprintf("invalid input_schema: %v", err))
			return
		}
		if _, err := compiler.Compile("schema.json"); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schema", fmt.Sprintf("failed to compile input_schema: %v", err))
			return
		}
	}
	if len(req.IdentityBindings) == 0 {
		req.IdentityBindings = []byte("{}")
	}
	if len(req.Executor) == 0 {
		req.Executor = []byte("{}")
	}
	if len(req.DownstreamAuth) == 0 {
		req.DownstreamAuth = []byte("{}")
	}
	if len(req.ResultConfig) == 0 {
		req.ResultConfig = []byte("{}")
	}
	if len(req.CacheConfig) == 0 {
		req.CacheConfig = []byte("{}")
	}

	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}

	tool, err := h.svc.queries.CreateTool(r.Context(), db.CreateToolParams{
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
	_, err = h.svc.queries.CreateToolVersion(r.Context(), db.CreateToolVersionParams{
		ID:               versionID,
		ToolID:           req.ID,
		ProjectID:        projectID,
		Environment:      env,
		Version:          1,
		SideEffect:       req.SideEffect,
		InputSchema:      req.InputSchema,
		IdentityBindings: req.IdentityBindings,
		Executor:         req.Executor,
		DownstreamAuth:   req.DownstreamAuth,
		Confirmation:     req.Confirmation,
		Authorization:    req.Authorization,
		ResultConfig:     req.ResultConfig,
		CacheConfig:      req.CacheConfig,
		IsActive:         true,
	})
	if err != nil {
		_ = h.svc.queries.DeleteTool(r.Context(), req.ID)
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_tool", "tool", tool.ID, req)

	writeJSON(w, http.StatusCreated, tool)
}

func (h *Handler) HandleGetTool(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	toolID := chi.URLParam(r, "id")
	tool, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

	versions, err := h.svc.queries.ListToolVersions(r.Context(), toolID)
	if err != nil {
		versions = []db.ToolVersions{}
	}

	writeJSON(w, http.StatusOK, ToolDetailsResponse{
		Tool:     tool,
		Versions: versions,
	})
}

func (h *Handler) HandleUpdateTool(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	toolID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req CreateToolRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

	newVersion := existing.ActiveVersion + 1
	versionID := fmt.Sprintf("%s_v%d", toolID, newVersion)

	if req.SideEffect == "" {
		req.SideEffect = "read"
	}
	if req.Confirmation == "" {
		req.Confirmation = "none"
	}
	if len(req.InputSchema) == 0 {
		req.InputSchema = []byte("{}")
	}
	if len(req.IdentityBindings) == 0 {
		req.IdentityBindings = []byte("{}")
	}
	if len(req.Executor) == 0 {
		req.Executor = []byte("{}")
	}
	if len(req.DownstreamAuth) == 0 {
		req.DownstreamAuth = []byte("{}")
	}
	if len(req.ResultConfig) == 0 {
		req.ResultConfig = []byte("{}")
	}
	if len(req.CacheConfig) == 0 {
		req.CacheConfig = []byte("{}")
	}

	_, err = h.svc.queries.CreateToolVersion(r.Context(), db.CreateToolVersionParams{
		ID:               versionID,
		ToolID:           toolID,
		ProjectID:        projectID,
		Environment:      env,
		Version:          newVersion,
		SideEffect:       req.SideEffect,
		InputSchema:      req.InputSchema,
		IdentityBindings: req.IdentityBindings,
		Executor:         req.Executor,
		DownstreamAuth:   req.DownstreamAuth,
		Confirmation:     req.Confirmation,
		Authorization:    req.Authorization,
		ResultConfig:     req.ResultConfig,
		CacheConfig:      req.CacheConfig,
		IsActive:         true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	updated, err := h.svc.queries.UpdateToolActiveVersion(r.Context(), db.UpdateToolActiveVersionParams{
		ID:            toolID,
		ActiveVersion: newVersion,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "update_tool", "tool", toolID, req)

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) HandleDeleteTool(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	toolID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	if err := h.svc.queries.DeleteTool(r.Context(), toolID); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_tool", "tool", toolID, nil)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": toolID})
}

// --- Tool Secrets ---

type UpsertToolSecretRequest struct {
	SecretValue string `json:"secret_value"`
}

func (h *Handler) HandleUpsertToolSecret(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	toolID := chi.URLParam(r, "id")
	secretRef := chi.URLParam(r, "secret_ref")
	projectID, env := getProjectAndEnv(r)

	var req UpsertToolSecretRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if toolID == "" || secretRef == "" || strings.TrimSpace(req.SecretValue) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "tool id, secret_ref, and secret_value are required")
		return
	}

	key := h.svc.masterKey
	if len(key) == 0 {
		key = []byte("default-master-key-32-chars-long")
	}
	derivedKey := secrets.DeriveKey(string(key))

	ciphertext, nonce, err := secrets.Encrypt(derivedKey, []byte(req.SecretValue))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encryption_error", "failed to encrypt secret")
		return
	}

	keyID := fmt.Sprintf("%x", sha256.Sum256(derivedKey))[:8]
	secretID := fmt.Sprintf("%s_%s_%s_%s", projectID, env, toolID, secretRef)

	_, err = h.svc.queries.UpsertToolSecret(r.Context(), db.UpsertToolSecretParams{
		ID:              secretID,
		ProjectID:       projectID,
		Environment:     env,
		ToolID:          toolID,
		SecretRef:       secretRef,
		EncryptedSecret: ciphertext,
		Nonce:           nonce,
		KeyID:           keyID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "upsert_tool_secret", "tool_secret", secretRef, map[string]string{
		"tool_id":    toolID,
		"secret_ref": secretRef,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "saved",
		"tool_id":    toolID,
		"secret_ref": secretRef,
	})
}
