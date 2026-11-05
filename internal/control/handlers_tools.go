package control

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/control/auth"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/secrets"
	"github.com/beavend/beavend/internal/store/db"
	"github.com/beavend/beavend/internal/tools"
	chi "github.com/go-chi/chi/v5"
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
	if !validateToolPayload(w, &req) {
		return
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
	projectID, env := getProjectAndEnv(r)
	tool, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil || tool.ProjectID != projectID || tool.Environment != env {
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
	toolID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req CreateToolRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.SideEffect == "" {
		req.SideEffect = "read"
	}
	if req.Confirmation == "" {
		req.Confirmation = "none"
	}
	if !validateToolPayload(w, &req) {
		return
	}

	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}

	existing, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil || existing.ProjectID != projectID || existing.Environment != env {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

	newVersion := existing.ActiveVersion + 1
	versionID := fmt.Sprintf("%s_v%d", toolID, newVersion)
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

	existingTool, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil || existingTool.ProjectID != projectID || existingTool.Environment != env {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

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

	tool, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil || tool.ProjectID != projectID || tool.Environment != env {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

	key := h.svc.masterKey
	if len(key) == 0 {
		writeError(w, http.StatusInternalServerError, "server_configuration_error", "master key is not configured")
		return
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

func validateToolPayload(w http.ResponseWriter, req *CreateToolRequest) bool {
	var inputSchema map[string]any
	trimmedSchema := strings.TrimSpace(string(req.InputSchema))
	if len(req.InputSchema) > 0 && trimmedSchema != "" && trimmedSchema != "null" && trimmedSchema != "{}" {
		if err := json.Unmarshal(req.InputSchema, &inputSchema); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_schema", fmt.Sprintf("invalid input_schema json: %v", err))
			return false
		}
	} else {
		req.InputSchema = []byte("{}")
	}

	var execCfg core.ExecutorConfig
	if len(req.Executor) > 0 && string(req.Executor) != "{}" {
		if err := json.Unmarshal(req.Executor, &execCfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_executor", fmt.Sprintf("invalid executor json: %v", err))
			return false
		}
	}

	tDef := core.ToolDefinition{
		ID:          req.ID,
		Name:        req.Name,
		InputSchema: inputSchema,
		Executor:    execCfg,
	}

	if err := tools.ValidateToolDefinition(tDef); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "input schema") {
			writeError(w, http.StatusBadRequest, "invalid_schema", errStr)
		} else if strings.Contains(errStr, "sql template") || strings.Contains(errStr, "not permitted in read-only") || strings.Contains(errStr, "statement must start with") {
			writeError(w, http.StatusBadRequest, "invalid_sql_template", errStr)
		} else {
			writeError(w, http.StatusBadRequest, "invalid_executor", errStr)
		}
		return false
	}
	return true
}

type TestToolRequest struct {
	Arguments map[string]any `json:"arguments"`
	Identity  *core.Identity `json:"identity,omitempty"`
}

func (h *Handler) HandleTestTool(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	toolID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req TestToolRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	tool, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil || tool.ProjectID != projectID || tool.Environment != env {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

	tv, err := h.svc.queries.GetToolVersion(r.Context(), db.GetToolVersionParams{
		ToolID:  tool.ID,
		Version: tool.ActiveVersion,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "tool active version not found")
		return
	}

	var execCfg core.ExecutorConfig
	if len(tv.Executor) > 0 {
		_ = json.Unmarshal(tv.Executor, &execCfg)
	}

	var downstreamAuth core.DownstreamAuthConfig
	if len(tv.DownstreamAuth) > 0 {
		_ = json.Unmarshal(tv.DownstreamAuth, &downstreamAuth)
	}

	var identityBindings map[string]string
	if len(tv.IdentityBindings) > 0 {
		_ = json.Unmarshal(tv.IdentityBindings, &identityBindings)
	}

	tDef := core.ToolDefinition{
		ID:               tool.ID,
		Name:             tool.Name,
		Description:      tool.Description,
		Version:          int(tv.Version),
		SideEffect:       tv.SideEffect,
		IdentityBindings: identityBindings,
		Executor:         execCfg,
		DownstreamAuth:   downstreamAuth,
		Confirmation:     tv.Confirmation,
		Authorization:    tv.Authorization,
	}

	if tv.SideEffect == "write" {
		writeError(w, http.StatusForbidden, "write_tool_test_prohibited", "testing tools with side_effect=write via admin API is prohibited")
		return
	}

	var id core.Identity
	if req.Identity != nil {
		id = *req.Identity
	}
	if adminUser, ok := auth.GetAdminUser(r.Context()); ok && adminUser != nil {
		if id.Subject == "" || adminUser.Role != auth.RoleOwner {
			id.Subject = adminUser.ID
			id.Issuer = "beavend:admin"
		}
	}

	args := req.Arguments
	if args == nil {
		args = make(map[string]any)
	}
	if len(identityBindings) > 0 {
		args = tools.InjectIdentityBindings(args, identityBindings, &id)
	}

	argsBytes, _ := json.Marshal(args)
	toolCall := core.ToolCall{
		ID:        fmt.Sprintf("test_%s", tool.ID),
		Name:      tool.Name,
		Arguments: string(argsBytes),
	}

	var exec core.Executor
	switch execCfg.Type {
	case "http", "webhook", "":
		exec = tools.NewHTTPExecutor(tDef, nil, nil)
	case "mcp":
		if strings.EqualFold(execCfg.Transport, "stdio") || strings.TrimSpace(execCfg.Command) != "" {
			writeError(w, http.StatusBadRequest, "invalid_executor", "mcp stdio transport is disabled to prevent host execution")
			return
		}
		exec = tools.NewMCPExecutor(tDef, nil, nil)
	case "sql_template":
		exec = tools.NewSQLTemplateExecutor(tDef, nil)
	case "wasm":
		rt, err := tools.NewWASMRuntime(r.Context(), nil, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "wasm_init_error", err.Error())
			return
		}
		defer rt.Close(r.Context())
		exec = tools.NewWASMExecutor(tDef, rt)
	default:
		writeError(w, http.StatusBadRequest, "unsupported_executor", fmt.Sprintf("unsupported executor type: %s", execCfg.Type))
		return
	}

	res, err := exec.Execute(r.Context(), toolCall, id)
	if err != nil && res.Content == "" {
		writeError(w, http.StatusInternalServerError, "execution_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) HandleSetActiveToolVersion(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	toolID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req SetActiveVersionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "valid version is required")
		return
	}

	tool, err := h.svc.queries.GetTool(r.Context(), toolID)
	if err != nil || tool.ProjectID != projectID || tool.Environment != env {
		writeError(w, http.StatusNotFound, "not_found", "tool not found")
		return
	}

	_, err = h.svc.queries.GetToolVersion(r.Context(), db.GetToolVersionParams{
		ToolID:  toolID,
		Version: req.Version,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "tool version not found")
		return
	}

	updated, err := h.svc.queries.UpdateToolActiveVersion(r.Context(), db.UpdateToolActiveVersionParams{
		ID:            toolID,
		ActiveVersion: req.Version,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "set_active_tool_version", "tool", toolID, req)

	writeJSON(w, http.StatusOK, updated)
}

