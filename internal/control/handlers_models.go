package control

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/secrets"
	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type UpsertModelRoutingRequest struct {
	Deployment          string          `json:"deployment"`
	Adapter             string          `json:"adapter"`
	FallbackDeployments json.RawMessage `json:"fallback_deployments"`
	InputCostPer1M      *float64        `json:"input_cost_per_1m,omitempty"`
	OutputCostPer1M     *float64        `json:"output_cost_per_1m,omitempty"`
	InputModalities     []string        `json:"input_modalities,omitempty"`
	OutputModalities    []string        `json:"output_modalities,omitempty"`
}

type ModelRoutingResponse struct {
	ID                  string   `json:"id"`
	ProjectID           string   `json:"project_id"`
	Environment         string   `json:"environment"`
	LogicalModel        string   `json:"logical_model"`
	Deployment          string   `json:"deployment"`
	Adapter             string   `json:"adapter"`
	FallbackDeployments []string `json:"fallback_deployments"`
	InputCostPer1M      *float64 `json:"input_cost_per_1m,omitempty"`
	OutputCostPer1M     *float64 `json:"output_cost_per_1m,omitempty"`
	InputModalities     []string `json:"input_modalities,omitempty"`
	OutputModalities    []string `json:"output_modalities,omitempty"`
}

func toModelRoutingResponse(m db.ModelRoutings) ModelRoutingResponse {
	var fallbacks []string
	if len(m.FallbackDeployments) > 0 {
		if err := json.Unmarshal(m.FallbackDeployments, &fallbacks); err != nil {
			var rawObjects []map[string]any
			if objErr := json.Unmarshal(m.FallbackDeployments, &rawObjects); objErr == nil {
				for _, obj := range rawObjects {
					prov, _ := obj["provider"].(string)
					if prov == "" {
						prov, _ = obj["adapter"].(string)
					}
					dep, _ := obj["deployment"].(string)
					if dep == "" {
						dep, _ = obj["model"].(string)
					}
					if prov != "" && dep != "" {
						fallbacks = append(fallbacks, prov+"/"+dep)
					} else if dep != "" {
						fallbacks = append(fallbacks, dep)
					}
				}
			}
		}
	}
	if fallbacks == nil {
		fallbacks = []string{}
	}
	inMods := m.InputModalities
	if inMods == nil {
		inMods = []string{}
	}
	outMods := m.OutputModalities
	if outMods == nil {
		outMods = []string{}
	}
	return ModelRoutingResponse{
		ID:                  m.ID,
		ProjectID:           m.ProjectID,
		Environment:         m.Environment,
		LogicalModel:        m.LogicalModel,
		Deployment:          m.Deployment,
		Adapter:             m.Adapter,
		FallbackDeployments: fallbacks,
		InputCostPer1M:      numericToFloatPtr(m.InputCostPer1m),
		OutputCostPer1M:     numericToFloatPtr(m.OutputCostPer1m),
		InputModalities:     inMods,
		OutputModalities:    outMods,
	}
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

	resp := make([]ModelRoutingResponse, len(routings))
	for i, rt := range routings {
		resp[i] = toModelRoutingResponse(rt)
	}

	writeJSON(w, http.StatusOK, resp)
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

	if err := validateModalities("input_modalities", req.InputModalities); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	if err := validateModalities("output_modalities", req.OutputModalities); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}

	// Verify that at least one provider is configured
	providers, err := h.svc.queries.ListProviderConfigs(r.Context(), db.ListProviderConfigsParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err == nil && len(providers) == 0 {
		writeError(w, http.StatusBadRequest, "no_providers_configured", "cannot configure model routing: no provider credentials configured. Please add a provider first.")
		return
	}

	if req.Adapter == "" {
		if len(providers) > 0 {
			req.Adapter = providers[0].ProviderName
		} else {
			req.Adapter = "litellm"
		}
	}
	if len(req.FallbackDeployments) == 0 || string(req.FallbackDeployments) == "null" {
		req.FallbackDeployments = []byte("[]")
	}

	inMods := req.InputModalities
	if inMods == nil {
		inMods = []string{}
	}
	outMods := req.OutputModalities
	if outMods == nil {
		outMods = []string{}
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
		InputCostPer1m:      floatPtrToNumeric(req.InputCostPer1M),
		OutputCostPer1m:     floatPtrToNumeric(req.OutputCostPer1M),
		InputModalities:     inMods,
		OutputModalities:    outMods,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "upsert_model_routing", "model_routing", logicalModel, req)

	writeJSON(w, http.StatusOK, toModelRoutingResponse(routing))
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

type TestProviderConfigRequest struct {
	ProviderName string `json:"provider_name"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
}

type TestProviderConfigResponse struct {
	Success    bool   `json:"success"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMs  int64  `json:"latency_ms"`
	Message    string `json:"message"`
}

func (h *Handler) HandleTestProviderConfig(w http.ResponseWriter, r *http.Request) {
	projectID, env := getProjectAndEnv(r)

	var req TestProviderConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	providerName := strings.TrimSpace(req.ProviderName)
	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	apiKey := strings.TrimSpace(req.APIKey)

	// If apiKey is missing or masked, attempt to fetch stored key
	if (apiKey == "" || strings.Contains(apiKey, "•••")) && providerName != "" && h.svc.queries != nil {
		stored, err := h.svc.queries.GetProviderConfig(r.Context(), db.GetProviderConfigParams{
			ProjectID:    projectID,
			Environment:  env,
			ProviderName: providerName,
		})
		if err == nil {
			if baseURL == "" {
				baseURL = strings.TrimRight(strings.TrimSpace(stored.BaseUrl), "/")
			}
			key := h.svc.masterKey
			if len(key) == 0 {
				key = []byte("default-master-key-32-chars-long")
			}
			derivedKey := secrets.DeriveKey(string(key))
			decrypted, err := secrets.Decrypt(derivedKey, stored.EncryptedApiKey, stored.Nonce)
			if err == nil {
				apiKey = string(decrypted)
			}
		}
	}

	if baseURL == "" {
		if providerName == "litellm" {
			baseURL = "http://localhost:4000"
		} else {
			baseURL = "https://api.openai.com"
		}
	}

	var candidateURLs []string
	if providerName == "litellm" || strings.Contains(baseURL, "4000") {
		candidateURLs = []string{baseURL + "/health/readiness", baseURL + "/health"}
	} else if strings.HasSuffix(baseURL, "/v1") {
		candidateURLs = []string{baseURL + "/models", strings.TrimSuffix(baseURL, "/v1") + "/models"}
	} else {
		candidateURLs = []string{baseURL + "/v1/models", baseURL + "/models", baseURL + "/api/tags"}
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	start := time.Now()
	var lastErr error
	var lastStatus int

	for _, testURL := range candidateURLs {
		httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, testURL, nil)
		if err != nil {
			lastErr = err
			continue
		}

		httpReq.Header.Set("User-Agent", "Beavend/1.0")
		if strings.Contains(baseURL, "openrouter.ai") {
			httpReq.Header.Set("HTTP-Referer", "https://github.com/beavend/beavend")
			httpReq.Header.Set("X-Title", "Beavend")
		}
		if apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		latency := time.Since(start).Milliseconds()
		lastStatus = resp.StatusCode
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			writeJSON(w, http.StatusOK, TestProviderConfigResponse{
				Success:    true,
				StatusCode: resp.StatusCode,
				LatencyMs:  latency,
				Message:    fmt.Sprintf("Connected successfully to %s (%dms)", providerName, latency),
			})
			return
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			writeJSON(w, http.StatusOK, TestProviderConfigResponse{
				Success:    false,
				StatusCode: resp.StatusCode,
				LatencyMs:  latency,
				Message:    "Authentication failed: invalid or unauthorized API key",
			})
			return
		}
	}

	latency := time.Since(start).Milliseconds()
	if lastErr != nil && lastStatus == 0 {
		writeJSON(w, http.StatusOK, TestProviderConfigResponse{
			Success:   false,
			LatencyMs: latency,
			Message:   fmt.Sprintf("Connection failed: %v", lastErr),
		})
		return
	}

	writeJSON(w, http.StatusOK, TestProviderConfigResponse{
		Success:    false,
		StatusCode: lastStatus,
		LatencyMs:  latency,
		Message:    fmt.Sprintf("Provider returned HTTP status %d", lastStatus),
	})
}

type FetchProviderModelsResponse struct {
	ProviderName string   `json:"provider_name"`
	Models       []string `json:"models"`
}

func parseModelList(body []byte) []string {
	var seen = make(map[string]bool)
	var models []string

	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}

	// 1. Try JSON object
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err == nil {
		if rawData, ok := obj["data"].([]any); ok {
			for _, item := range rawData {
				switch v := item.(type) {
				case string:
					add(v)
				case map[string]any:
					if id, ok := v["id"].(string); ok {
						add(id)
					} else if name, ok := v["name"].(string); ok {
						add(name)
					}
				}
			}
		}
		if rawModels, ok := obj["models"].([]any); ok {
			for _, item := range rawModels {
				switch v := item.(type) {
				case string:
					add(v)
				case map[string]any:
					if id, ok := v["id"].(string); ok {
						add(id)
					} else if name, ok := v["name"].(string); ok {
						add(name)
					} else if model, ok := v["model"].(string); ok {
						add(model)
					}
				}
			}
		}
	}

	// 2. Try JSON list
	if len(models) == 0 {
		var list []any
		if err := json.Unmarshal(body, &list); err == nil {
			for _, item := range list {
				switch v := item.(type) {
				case string:
					add(v)
				case map[string]any:
					if id, ok := v["id"].(string); ok {
						add(id)
					} else if name, ok := v["name"].(string); ok {
						add(name)
					}
				}
			}
		}
	}

	sort.Strings(models)
	return models
}

func (h *Handler) HandleFetchProviderModels(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	providerName := chi.URLParam(r, "provider_name")
	projectID, env := getProjectAndEnv(r)

	stored, err := h.svc.queries.GetProviderConfig(r.Context(), db.GetProviderConfigParams{
		ProjectID:    projectID,
		Environment:  env,
		ProviderName: providerName,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "provider_not_found", fmt.Sprintf("provider %q not found", providerName))
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(stored.BaseUrl), "/")
	if baseURL == "" {
		if providerName == "litellm" {
			baseURL = "http://localhost:4000"
		} else {
			baseURL = "https://api.openai.com"
		}
	}

	var apiKey string
	key := h.svc.masterKey
	if len(key) == 0 {
		key = []byte("default-master-key-32-chars-long")
	}
	derivedKey := secrets.DeriveKey(string(key))
	decrypted, err := secrets.Decrypt(derivedKey, stored.EncryptedApiKey, stored.Nonce)
	if err == nil {
		apiKey = string(decrypted)
	}

	var candidateURLs []string
	if strings.HasSuffix(baseURL, "/v1") {
		candidateURLs = append(candidateURLs, baseURL+"/models", strings.TrimSuffix(baseURL, "/v1")+"/models")
	} else {
		candidateURLs = append(candidateURLs, baseURL+"/v1/models", baseURL+"/models", baseURL+"/api/tags")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var lastErr string
	for _, targetURL := range candidateURLs {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)
		if err != nil {
			lastErr = err.Error()
			continue
		}

		req.Header.Set("User-Agent", "Beavend/1.0")
		if strings.Contains(baseURL, "openrouter.ai") {
			req.Header.Set("HTTP-Referer", "https://github.com/beavend/beavend")
			req.Header.Set("X-Title", "Beavend")
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err.Error()
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			models := parseModelList(body)
			writeJSON(w, http.StatusOK, FetchProviderModelsResponse{
				ProviderName: providerName,
				Models:       models,
			})
			return
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			writeError(w, http.StatusUnauthorized, "provider_unauthorized", "Authentication failed: invalid or unauthorized API key")
			return
		}

		lastErr = fmt.Sprintf("HTTP %d from %s", resp.StatusCode, targetURL)
	}

	writeError(w, http.StatusBadGateway, "provider_fetch_failed", fmt.Sprintf("Failed to fetch models from provider %q: %s", providerName, lastErr))
}

func floatPtrToNumeric(f *float64) pgtype.Numeric {
	if f == nil {
		return pgtype.Numeric{Valid: false}
	}
	var num pgtype.Numeric
	_ = num.Scan(fmt.Sprintf("%.6f", *f))
	return num
}

func numericToFloatPtr(num pgtype.Numeric) *float64 {
	if !num.Valid {
		return nil
	}
	f, err := num.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	val := f.Float64
	return &val
}


