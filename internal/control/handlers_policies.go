package control

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
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
	if !decodeJSON(w, r, &req) {
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
	if !decodeJSON(w, r, &req) {
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

type TestPolicyRequest struct {
	Expression string         `json:"expression"`
	Claims     map[string]any `json:"claims"`
	Identity   map[string]any `json:"identity"`
	Meta       map[string]any `json:"meta"`
}

type TestPolicyResponse struct {
	Valid   bool   `json:"valid"`
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`
}

func (h *Handler) HandleTestPolicy(w http.ResponseWriter, r *http.Request) {
	var req TestPolicyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Expression = strings.TrimSpace(req.Expression)
	if req.Expression == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "expression is required")
		return
	}

	if h.svc.policyCompiler == nil {
		writeError(w, http.StatusInternalServerError, "compiler_unavailable", "policy compiler not initialized")
		return
	}

	prg, err := h.svc.policyCompiler.Compile(req.Expression)
	if err != nil {
		writeJSON(w, http.StatusOK, TestPolicyResponse{
			Valid:   false,
			Allowed: false,
			Error:   err.Error(),
		})
		return
	}

	id := &core.Identity{
		Claims: req.Claims,
	}
	if req.Identity != nil {
		if v, ok := req.Identity["project"].(string); ok {
			id.Project = v
		}
		if v, ok := req.Identity["environment"].(string); ok {
			id.Environment = v
		}
		if v, ok := req.Identity["issuer"].(string); ok {
			id.Issuer = v
		}
		if v, ok := req.Identity["subject"].(string); ok {
			id.Subject = v
		}
		if v, ok := req.Identity["audience"].(string); ok {
			id.Audience = v
		}
	}
	if id.Project == "" {
		projectID, env := getProjectAndEnv(r)
		id.Project = projectID
		id.Environment = env
	}

	evaluator, err := policy.NewEvaluator(h.svc.policyCompiler)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "evaluator_error", err.Error())
		return
	}

	allowed, err := evaluator.EvaluateProgram(r.Context(), prg, id, req.Meta)
	if err != nil {
		writeJSON(w, http.StatusOK, TestPolicyResponse{
			Valid:   true,
			Allowed: false,
			Error:   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, TestPolicyResponse{
		Valid:   true,
		Allowed: allowed,
	})
}

type SetActiveVersionRequest struct {
	Version int32 `json:"version"`
}

func (h *Handler) HandleSetActivePolicyVersion(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	policyID := chi.URLParam(r, "id")
	projectID, env := getProjectAndEnv(r)

	var req SetActiveVersionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "valid version is required")
		return
	}

	_, err := h.svc.queries.GetPolicyVersion(r.Context(), db.GetPolicyVersionParams{
		PolicyID: policyID,
		Version:  req.Version,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "policy version not found")
		return
	}

	updated, err := h.svc.queries.UpdatePolicyActiveVersion(r.Context(), db.UpdatePolicyActiveVersionParams{
		ID:            policyID,
		ActiveVersion: req.Version,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	_, _ = h.svc.InvalidateSnapshot(r.Context(), projectID, env)
	h.svc.RecordAudit(r.Context(), projectID, env, "set_active_policy_version", "policy", policyID, req)

	writeJSON(w, http.StatusOK, updated)
}
