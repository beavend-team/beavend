package control

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type CreateAppAPIKeyRequest struct {
	Name          string `json:"name"`
	ExpiresInDays int    `json:"expires_in_days,omitempty"`
}

type AppAPIKeyResponse struct {
	ID          string  `json:"id"`
	AppID       string  `json:"app_id"`
	ProjectID   string  `json:"project_id"`
	Environment string  `json:"environment"`
	Name        string  `json:"name"`
	KeyPrefix   string  `json:"key_prefix"`
	Token       string  `json:"token,omitempty"` // Only populated upon creation
	CreatedAt   string  `json:"created_at"`
	LastUsedAt  *string `json:"last_used_at,omitempty"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
}

func (h *Handler) HandleListAppAPIKeys(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	appID := strings.TrimSpace(chi.URLParam(r, "id"))
	if appID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "app id is required")
		return
	}

	keys, err := h.svc.queries.ListAppAPIKeys(r.Context(), db.ListAppAPIKeysParams{
		ProjectID:   projectID,
		Environment: env,
		AppID:       appID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	var resp []AppAPIKeyResponse
	for _, k := range keys {
		var lastUsed, expires *string
		if k.LastUsedAt.Valid {
			s := k.LastUsedAt.Time.Format(time.RFC3339)
			lastUsed = &s
		}
		if k.ExpiresAt.Valid {
			s := k.ExpiresAt.Time.Format(time.RFC3339)
			expires = &s
		}
		resp = append(resp, AppAPIKeyResponse{
			ID:          k.ID,
			AppID:       k.AppID,
			ProjectID:   k.ProjectID,
			Environment: k.Environment,
			Name:        k.Name,
			KeyPrefix:   k.KeyPrefix,
			CreatedAt:   k.CreatedAt.Time.Format(time.RFC3339),
			LastUsedAt:  lastUsed,
			ExpiresAt:   expires,
		})
	}

	if resp == nil {
		resp = []AppAPIKeyResponse{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleCreateAppAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	appID := strings.TrimSpace(chi.URLParam(r, "id"))
	if appID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "app id is required")
		return
	}

	var req CreateAppAPIKeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "Default API Key"
	}

	// Generate 24 bytes of random entropy
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "crypto_error", "failed to generate key entropy")
		return
	}

	rawEntropy := hex.EncodeToString(randomBytes)
	fullToken := "bv_app_sec_" + rawEntropy
	keyPrefix := "bv_app_..." + rawEntropy[:6]

	// Compute SHA-256 hash for secure storage and constant-time lookup
	hashBytes := sha256.Sum256([]byte(fullToken))
	keyHash := hex.EncodeToString(hashBytes[:])

	keyID := fmt.Sprintf("key_%x", time.Now().UnixNano())

	var expiresAt pgtype.Timestamptz
	if req.ExpiresInDays > 0 {
		expTime := time.Now().AddDate(0, 0, req.ExpiresInDays)
		expiresAt = pgtype.Timestamptz{Time: expTime, Valid: true}
	}

	created, err := h.svc.queries.CreateAppAPIKey(r.Context(), db.CreateAppAPIKeyParams{
		ID:          keyID,
		AppID:       appID,
		ProjectID:   projectID,
		Environment: env,
		Name:        req.Name,
		KeyPrefix:   keyPrefix,
		KeyHash:     keyHash,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	// Invalidate snapshot so runtime plane immediately learns about this key
	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_app_api_key", "app_api_key", keyID, map[string]any{
		"app_id": appID,
		"name":   req.Name,
		"prefix": keyPrefix,
	})

	var expStr *string
	if created.ExpiresAt.Valid {
		s := created.ExpiresAt.Time.Format(time.RFC3339)
		expStr = &s
	}

	writeJSON(w, http.StatusCreated, AppAPIKeyResponse{
		ID:          created.ID,
		AppID:       created.AppID,
		ProjectID:   created.ProjectID,
		Environment: created.Environment,
		Name:        created.Name,
		KeyPrefix:   created.KeyPrefix,
		Token:       fullToken, // Return the full secret once
		CreatedAt:   created.CreatedAt.Time.Format(time.RFC3339),
		ExpiresAt:   expStr,
	})
}

func (h *Handler) HandleDeleteAppAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	appID := strings.TrimSpace(chi.URLParam(r, "id"))
	keyID := strings.TrimSpace(chi.URLParam(r, "key_id"))
	if appID == "" || keyID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "app id and key id are required")
		return
	}

	err := h.svc.queries.DeleteAppAPIKey(r.Context(), db.DeleteAppAPIKeyParams{
		ID:    keyID,
		AppID: appID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	// Invalidate snapshot so runtime plane immediately drops revoked key
	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_app_api_key", "app_api_key", keyID, map[string]any{
		"app_id": appID,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "revoked",
	})
}
