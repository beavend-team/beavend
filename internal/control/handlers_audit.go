package control

import (
	"net/http"
	"strconv"

	"github.com/beavend/beavend/internal/store/db"
)

// HandleListAuditLogs returns paginated audit log events.
func (h *Handler) HandleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	limit := int32(50)
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.ParseInt(lStr, 10, 32); err == nil && l > 0 && l <= 500 {
			limit = int32(l)
		}
	}

	offset := int32(0)
	if oStr := r.URL.Query().Get("offset"); oStr != "" {
		if o, err := strconv.ParseInt(oStr, 10, 32); err == nil && o >= 0 {
			offset = int32(o)
		}
	}

	projectID := r.URL.Query().Get("project_id")
	env := r.URL.Query().Get("environment")

	if projectID != "" && env != "" {
		logs, err := h.svc.queries.ListAuditLogsByProjectEnv(r.Context(), db.ListAuditLogsByProjectEnvParams{
			ProjectID:   projectID,
			Environment: env,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, logs)
		return
	}

	logs, err := h.svc.queries.ListAuditLogs(r.Context(), db.ListAuditLogsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, logs)
}
