package control

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/configcode"
	"github.com/go-chi/chi/v5"
)

// HandleExportConfig exports the declarative configuration document for a project environment as YAML.
func (h *Handler) HandleExportConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil || h.svc.ConfigEngine() == nil {
		writeError(w, http.StatusInternalServerError, "service_unavailable", "config engine not configured")
		return
	}

	projectID := chi.URLParam(r, "project_id")
	env := chi.URLParam(r, "name")
	if env == "" {
		env = chi.URLParam(r, "env")
	}

	doc, err := h.svc.ConfigEngine().Export(r.Context(), projectID, env)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_error", err.Error())
		return
	}

	yamlBytes, err := doc.ToYAML()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "yaml_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s.yaml\"", projectID, env))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(yamlBytes)
}

// HandleImportConfig validates and applies an imported configuration document (YAML or JSON).
// Supports ?dry_run=true query parameter to preview changes without modifying the database.
func (h *Handler) HandleImportConfig(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil || h.svc.ConfigEngine() == nil {
		writeError(w, http.StatusInternalServerError, "service_unavailable", "config engine not configured")
		return
	}

	projectID := chi.URLParam(r, "project_id")
	env := chi.URLParam(r, "name")
	if env == "" {
		env = chi.URLParam(r, "env")
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "failed to read request body")
		return
	}

	var doc *configcode.ProjectDocument
	trimmed := strings.TrimSpace(string(bodyBytes))
	if strings.HasPrefix(trimmed, "{") {
		var d configcode.ProjectDocument
		if err := json.Unmarshal(bodyBytes, &d); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("invalid json: %v", err))
			return
		}
		doc = &d
	} else {
		d, err := configcode.FromYAML(bodyBytes)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("invalid yaml: %v", err))
			return
		}
		doc = d
	}

	if doc.Project.ID != "" && doc.Project.ID != projectID {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("project id in document (%q) does not match path (%q)", doc.Project.ID, projectID))
		return
	}
	if doc.Environment != "" && doc.Environment != env {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("environment in document (%q) does not match path (%q)", doc.Environment, env))
		return
	}
	doc.Project.ID = projectID
	doc.Environment = env

	dryRun := r.URL.Query().Get("dry_run") == "true"
	if dryRun {
		diff, err := h.svc.ConfigEngine().Diff(r.Context(), doc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "diff_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, diff)
		return
	}

	report, err := h.svc.ConfigEngine().Apply(r.Context(), doc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}

	h.svc.RecordAudit(
		r.Context(),
		projectID,
		env,
		"import_configuration",
		"project_environment",
		fmt.Sprintf("%s/%s", projectID, env),
		map[string]any{
			"summary": report.Summary,
		},
	)

	writeJSON(w, http.StatusOK, report)
}
