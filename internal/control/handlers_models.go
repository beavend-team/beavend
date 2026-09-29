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
)

type UpsertModelRoutingRequest struct {
	Deployment          string          `json:"deployment"`
	Adapter             string          `json:"adapter"`
	FallbackDeployments json.RawMessage `json:"fallback_deployments"`
}

func (h *Handler) HandleListModelRoutings(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	routings, err := h.svc.queries.ListModelRoutings(r.Context(), db.ListModelRoutingsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, routings)
}

func (h *Handler) HandleUpsertModelRouting(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	logicalModel := chi.URLParam(r, "logical_model")
	projectID, env := getProjectAndEnv(r)

	var req UpsertModelRoutingRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Adapter == "" {
		req.Adapter = "litellm"
	}
	if len(req.FallbackDeployments) == 0 {
		req.FallbackDeployments = []byte("[]")
	}

	routingID := fmt.Sprintf("%s_%s_%s", projectID, env, logicalModel)
	routing, err := h.svc.queries.UpsertModelRouting(r.Context(), db.UpsertModelRoutingParams{
		ID:                  routingID,
		ProjectID:           projectID,
		Environment:         env,
		LogicalModel:        logicalModel,
		Deployment:          req.Deployment,
		Adapter:             req.Adapter,
		FallbackDeployments: req.FallbackDeployments,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "upsert_model_routing", "model_routing", logicalModel, req)

	writeJSON(w, http.StatusOK, routing)
}

func (h *Handler) HandleDeleteModelRouting(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	logicalModel := chi.URLParam(r, "logical_model")
	projectID, env := getProjectAndEnv(r)

	if err := h.svc.queries.DeleteModelRouting(r.Context(), db.DeleteModelRoutingParams{
		ProjectID:    projectID,
		Environment:  env,
		LogicalModel: logicalModel,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_model_routing", "model_routing", logicalModel, nil)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "logical_model": logicalModel})
}

// --- Provider Configs ---

type UpsertProviderConfigRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type ProviderConfigResponse struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	Environment  string `json:"environment"`
	ProviderName string `json:"provider_name"`
	BaseURL      string `json:"base_url"`
	MaskedAPIKey string `json:"masked_api_key"`
}

func (h *Handler) HandleListProviderConfigs(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	configs, err := h.svc.queries.ListProviderConfigs(r.Context(), db.ListProviderConfigsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	resp := make([]ProviderConfigResponse, len(configs))
	for i, c := range configs {
		resp[i] = ProviderConfigResponse{
			ID:           c.ID,
			ProjectID:    c.ProjectID,
			Environment:  c.Environment,
			ProviderName: c.ProviderName,
			BaseURL:      c.BaseUrl,
			MaskedAPIKey: "••••••••",
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleUpsertProviderConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	providerName := chi.URLParam(r, "provider_name")
	projectID, env := getProjectAndEnv(r)

	var req UpsertProviderConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if providerName == "" || strings.TrimSpace(req.APIKey) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider_name and api_key are required")
		return
	}

	key := h.svc.masterKey
	if len(key) == 0 {
		key = []byte("default-master-key-32-chars-long")
	}
	derivedKey := secrets.DeriveKey(string(key))

	ciphertext, nonce, err := secrets.Encrypt(derivedKey, []byte(req.APIKey))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encryption_error", "failed to encrypt credentials")
		return
	}

	keyID := fmt.Sprintf("%x", sha256.Sum256(derivedKey))[:8]
	configID := fmt.Sprintf("%s_%s_%s", projectID, env, providerName)

	pConfig, err := h.svc.queries.UpsertProviderConfig(r.Context(), db.UpsertProviderConfigParams{
		ID:              configID,
		ProjectID:       projectID,
		Environment:     env,
		ProviderName:    providerName,
		BaseUrl:         req.BaseURL,
		EncryptedApiKey: ciphertext,
		Nonce:           nonce,
		KeyID:           keyID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "upsert_provider_config", "provider_config", providerName, map[string]string{
		"base_url": req.BaseURL,
	})

	writeJSON(w, http.StatusOK, ProviderConfigResponse{
		ID:           pConfig.ID,
		ProjectID:    pConfig.ProjectID,
		Environment:  pConfig.Environment,
		ProviderName: pConfig.ProviderName,
		BaseURL:      pConfig.BaseUrl,
		MaskedAPIKey: "••••••••",
	})
}

func (h *Handler) HandleDeleteProviderConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	providerName := chi.URLParam(r, "provider_name")
	projectID, env := getProjectAndEnv(r)

	if err := h.svc.queries.DeleteProviderConfig(r.Context(), db.DeleteProviderConfigParams{
		ProjectID:    projectID,
		Environment:  env,
		ProviderName: providerName,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_provider_config", "provider_config", providerName, nil)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "provider_name": providerName})
}
