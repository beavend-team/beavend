package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/core"
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

var validModalities = map[string]bool{"text": true, "image": true, "audio": true, "video": true, "document": true}

// validateModalities rejects unknown modality names.
func validateModalities(field string, mods []string) error {
	for _, m := range mods {
		if !validModalities[m] {
			return fmt.Errorf("%s contains unsupported modality %q; must be one of: text, image, audio, video, document", field, m)
		}
	}
	return nil
}

// validateModelRoutesShape performs structural validation of a model_routes payload.
// Cross-reference checks (unknown targets, capability mismatches) happen in the snapshot compiler on publish.
func validateModelRoutesShape(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var mr core.ModelRoutes
	if err := json.Unmarshal(raw, &mr); err != nil {
		return fmt.Errorf("invalid model_routes: %w", err)
	}
	if strings.TrimSpace(mr.Default) == "" {
		return errors.New("model_routes.default is required")
	}
	for i, rule := range mr.Rules {
		if strings.TrimSpace(rule.Use) == "" {
			return fmt.Errorf("model_routes.rules[%d].use is required", i)
		}
		c := rule.When
		if len(c.InputHas) == 0 && len(c.Output) == 0 && len(c.Metadata) == 0 && strings.TrimSpace(c.Expr) == "" {
			return fmt.Errorf("model_routes.rules[%d].when must specify at least one condition", i)
		}
		if err := validateModalities(fmt.Sprintf("model_routes.rules[%d].when.input_has", i), c.InputHas); err != nil {
			return err
		}
		if err := validateModalities(fmt.Sprintf("model_routes.rules[%d].when.output", i), c.Output); err != nil {
			return err
		}
	}
	return nil
}
