package control

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/store/db"
	"github.com/beavend/beavend/internal/usage"
	chi "github.com/go-chi/chi/v5"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// HandleGetUsage queries aggregated usage rollups.
func (h *Handler) HandleGetUsage(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)
	granularity := r.URL.Query().Get("granularity")
	if granularity == "" {
		granularity = "hour"
	}

	fromTime := time.Now().UTC().Add(-24 * time.Hour)
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			fromTime = t
		}
	}

	toTime := time.Now().UTC()
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			toTime = t
		}
	}

	// Synchronize recent usage events into rollups for the requested timeframe and active buckets
	// so the Usage & Cost page immediately reflects any recent AI hits without waiting for background cron.
	rollupStore := usage.NewPostgresRollupStore(h.svc.queries)
	agg := usage.NewRollupAggregator(rollupStore, usage.DefaultRollupAggregatorConfig())
	_ = agg.SyncRange(r.Context(), granularity, fromTime, toTime)

	rollups, err := h.svc.queries.GetUsageRollups(r.Context(), db.GetUsageRollupsParams{
		ProjectID:   projectID,
		Environment: env,
		Granularity: granularity,
		BucketStart: pgtype.Timestamptz{
			Time:  fromTime,
			Valid: true,
		},
		BucketStart_2: pgtype.Timestamptz{
			Time:  toTime,
			Valid: true,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rollups)
}

// HandleGetLogs queries request metadata logs with pagination.
func (h *Handler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	projectID, env := getProjectAndEnv(r)

	fromTime := time.Now().UTC().Add(-24 * time.Hour)
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			fromTime = t
		}
	}

	toTime := time.Now().UTC()
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			toTime = t
		}
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

	logs, err := h.svc.queries.GetRequestLogs(r.Context(), db.GetRequestLogsParams{
		ProjectID:   projectID,
		Environment: env,
		CreatedAt: pgtype.Timestamptz{
			Time:  fromTime,
			Valid: true,
		},
		CreatedAt_2: pgtype.Timestamptz{
			Time:  toTime,
			Valid: true,
		},
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, logs)
}

// HandleGetLogByID retrieves detailed metadata for a single request.
func (h *Handler) HandleGetLogByID(w http.ResponseWriter, r *http.Request) {
	if h.svc.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database not configured")
		return
	}
	requestID := chi.URLParam(r, "request_id")
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "request_id required")
		return
	}

	logEntry, err := h.svc.queries.GetRequestLogByID(r.Context(), requestID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "request log entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, logEntry)
}

// HandlePrivacyPurgeSubject deletes all request logs, usage events, and rollups for a subject.
func (h *Handler) HandlePrivacyPurgeSubject(w http.ResponseWriter, r *http.Request) {
	issuer := chi.URLParam(r, "issuer")
	if unescaped, err := url.PathUnescape(issuer); err == nil && unescaped != "" {
		issuer = unescaped
	}
	subject := chi.URLParam(r, "subject")
	if unescaped, err := url.PathUnescape(subject); err == nil && unescaped != "" {
		subject = unescaped
	}
	projectID, env := getProjectAndEnv(r)

	if subject == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "subject required")
		return
	}

	var purgeErrs []string
	if h.svc.queries != nil {
		// Purge request logs
		if err := h.svc.queries.DeleteRequestLogsBySubject(r.Context(), db.DeleteRequestLogsBySubjectParams{
			ProjectID:   projectID,
			Environment: env,
			UserSubject: subject,
		}); err != nil {
			purgeErrs = append(purgeErrs, fmt.Sprintf("request_logs: %v", err))
		}

		// Purge usage events
		if err := h.svc.queries.DeleteUsageEventsBySubject(r.Context(), db.DeleteUsageEventsBySubjectParams{
			ProjectID:   projectID,
			Environment: env,
			UserID:      subject,
		}); err != nil {
			purgeErrs = append(purgeErrs, fmt.Sprintf("usage_events: %v", err))
		}

		// Purge usage rollups
		if err := h.svc.queries.DeleteUsageRollupsBySubject(r.Context(), db.DeleteUsageRollupsBySubjectParams{
			ProjectID:   projectID,
			Environment: env,
			UserID:      subject,
		}); err != nil {
			purgeErrs = append(purgeErrs, fmt.Sprintf("usage_rollups: %v", err))
		}
	}

	// Purge cached responses and tool results
	if err := h.svc.PurgeSubjectCache(r.Context(), projectID, env, subject); err != nil {
		purgeErrs = append(purgeErrs, fmt.Sprintf("cache: %v", err))
	}

	// Purge semantic memories and conversation threads
	if err := h.svc.PurgeSubjectMemory(r.Context(), projectID, env, issuer, subject); err != nil {
		purgeErrs = append(purgeErrs, fmt.Sprintf("memory_threads: %v", err))
	}

	if len(purgeErrs) > 0 {
		writeError(w, http.StatusInternalServerError, "purge_error", strings.Join(purgeErrs, "; "))
		return
	}

	h.svc.RecordAudit(r.Context(), projectID, env, "privacy_purge_subject", "subject", subject, map[string]string{
		"issuer":  issuer,
		"subject": subject,
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "purged",
		"issuer":  issuer,
		"subject": subject,
	})
}
