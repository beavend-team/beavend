package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/store/db"
	"github.com/go-chi/chi/v5"
)

type CreatePolicyRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
}

type CreatePolicyVersionRequest struct {
	Expression string `json:"expression"`
	SetActive  bool   `json:"set_active"`
}

type PolicyDetailsResponse struct {
	Policy   db.Policies         `json:"policy"`
	Versions []db.PolicyVersions `json:"versions"`
}

func (h *Handler) HandleListPolicies(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	policies, err := h.svc.queries.ListPolicies(r.Context(), db.ListPoliciesParams{
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, policies)
}

func (h *Handler) HandleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	projectID, env := getProjectAndEnv(r)
	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Expression = strings.TrimSpace(req.Expression)
	if req.ID == "" || req.Name == "" || req.Expression == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id, name, and expression are required")
		return
	}

	// Validate CEL expression syntax & type-checking
	if h.svc.policyCompiler != nil {
		if _, err := h.svc.policyCompiler.Compile(req.Expression); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cel_expression", fmt.Sprintf("failed to compile CEL expression: %v", err))
			return
		}
	}

	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}

	policy, err := h.svc.queries.CreatePolicy(r.Context(), db.CreatePolicyParams{
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
	_, err = h.svc.queries.CreatePolicyVersion(r.Context(), db.CreatePolicyVersionParams{
		ID:          versionID,
		PolicyID:    req.ID,
		ProjectID:   projectID,
		Environment: env,
		Version:     1,
		Expression:  req.Expression,
		IsActive:    true,
	})
	if err != nil {
		_ = h.svc.queries.DeletePolicy(r.Context(), req.ID)
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_policy", "policy", policy.ID, req)

	writeJSON(w, http.StatusCreated, policy)
}

func (h *Handler) HandleGetPolicy(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	policyID := chi.URLParam(r, "id")
	policy, err := h.svc.queries.GetPolicy(r.Context(), policyID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "policy not found")
		return
	}

	versions, err := h.svc.queries.ListPolicyVersions(r.Context(), policyID)
	if err != nil {
		versions = []db.PolicyVersions{}
	}

	writeJSON(w, http.StatusOK, PolicyDetailsResponse{
		Policy:   policy,
		Versions: versions,
	})
}

func (h *Handler) HandleCreatePolicyVersion(w http.ResponseWriter, r *http.Request) {
	policyID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req CreatePolicyVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return
	}

	req.Expression = strings.TrimSpace(req.Expression)
	if req.Expression == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "expression is required")
		return
	}

	// Validate CEL expression syntax & type-checking
	if h.svc.policyCompiler != nil {
		if _, err := h.svc.policyCompiler.Compile(req.Expression); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cel_expression", fmt.Sprintf("failed to compile CEL expression: %v", err))
			return
		}
	}

	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}

	existing, err := h.svc.queries.GetPolicy(r.Context(), policyID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "policy not found")
		return
	}

	newVersion := existing.ActiveVersion + 1
	versionID := fmt.Sprintf("%s_v%d", policyID, newVersion)

	createdVersion, err := h.svc.queries.CreatePolicyVersion(r.Context(), db.CreatePolicyVersionParams{
		ID:          versionID,
		PolicyID:    policyID,
		ProjectID:   projectID,
		Environment: env,
		Version:     newVersion,
		Expression:  req.Expression,
		IsActive:    req.SetActive,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	if req.SetActive {
		_, _ = h.svc.queries.UpdatePolicyActiveVersion(r.Context(), db.UpdatePolicyActiveVersionParams{
			ID:            policyID,
			ActiveVersion: newVersion,
		})
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "create_policy_version", "policy", policyID, req)

	writeJSON(w, http.StatusCreated, createdVersion)
}

func (h *Handler) HandleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	policyID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	if err := h.svc.queries.DeletePolicy(r.Context(), policyID); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "delete_policy", "policy", policyID, nil)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": policyID})
}
