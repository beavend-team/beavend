package control

import (
	"encoding/json"
	"net/http"
	"strings"
)

// getProjectAndEnv extracts the tenant context from HTTP headers or query parameters.
func getProjectAndEnv(r *http.Request) (string, string) {
	projectID := strings.TrimSpace(r.Header.Get("X-Project-ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(r.URL.Query().Get("project_id"))
	}
	if projectID == "" {
		projectID = "default"
	}

	env := strings.TrimSpace(r.Header.Get("X-Environment"))
	if env == "" {
		env = strings.TrimSpace(r.URL.Query().Get("environment"))
	}
	if env == "" {
		env = "development"
	}

	return projectID, env
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}
