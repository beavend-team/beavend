package control

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/tools"
)

type TestSearchRequest struct {
	Query      string   `json:"query"`
	Categories []string `json:"categories,omitempty"`
	Engines    []string `json:"engines,omitempty"`
	MaxResults int      `json:"max_results,omitempty"`
}

func (h *Handler) HandleGetSearchStatus(w http.ResponseWriter, r *http.Request) {
	client := h.svc.SearXNGClient()
	if client == nil {
		writeJSON(w, http.StatusOK, tools.SearXNGHealth{
			Configured: false,
			Enabled:    false,
			Status:     "disabled",
			Error:      "SearXNG client is not initialized",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	health := client.CheckHealth(ctx)
	writeJSON(w, http.StatusOK, health)
}

func (h *Handler) HandleTestSearch(w http.ResponseWriter, r *http.Request) {
	client := h.svc.SearXNGClient()
	if client == nil || !client.IsEnabled() {
		writeError(w, http.StatusBadRequest, "search_disabled", "SearXNG search service is disabled or not configured")
		return
	}

	var req TestSearchRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "query cannot be empty")
		return
	}

	if req.MaxResults <= 0 {
		req.MaxResults = 5
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	res, err := client.Search(ctx, req.Query, req.Categories, req.Engines, "", 0, req.MaxResults)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, res)
}

type UpdateSearchConfigRequest struct {
	URL            string `json:"url"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func (h *Handler) HandleUpdateSearchConfig(w http.ResponseWriter, r *http.Request) {
	client := h.svc.SearXNGClient()
	if client == nil {
		writeError(w, http.StatusInternalServerError, "not_available", "SearXNG client not initialized")
		return
	}

	var req UpdateSearchConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	timeout := 8 * time.Second
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}

	cleanURL := strings.TrimSpace(req.URL)
	client.UpdateConfig(cleanURL, req.Enabled, timeout)

	projectID, env := getProjectAndEnv(r)
	h.svc.RecordAudit(r.Context(), projectID, env, "update_search_config", "search", "searxng", map[string]any{
		"url":     cleanURL,
		"enabled": req.Enabled,
	})

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	health := client.CheckHealth(ctx)
	writeJSON(w, http.StatusOK, health)
}

