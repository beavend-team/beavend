package control

import (
	"encoding/json"
	"errors"
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

// decodeJSON decodes JSON request bodies, gracefully returning 413 for oversized payloads or 400 for bad JSON.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := json.NewDecoder(r.Body).Decode(dst)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) || strings.Contains(err.Error(), "http: request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body exceeds maximum allowed size")
			return false
		}
		writeError(w, http.StatusBadRequest, "bad_request", "invalid request body")
		return false
	}
	return true
}
