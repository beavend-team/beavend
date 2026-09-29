package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/store/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// Handler handles admin authentication and user management HTTP endpoints.
type Handler struct {
	queries      *db.Queries
	tokenManager *TokenManager
	auditor      audit.Auditor
	secureCookie bool
}

// NewHandler constructs an auth HTTP handler.
func NewHandler(queries *db.Queries, tm *TokenManager, auditor audit.Auditor, secureCookie bool) *Handler {
	return &Handler{
		queries:      queries,
		tokenManager: tm,
		auditor:      auditor,
		secureCookie: secureCookie,
	}
}

// LoginRequest holds login credentials.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse contains issued session token and user info.
type LoginResponse struct {
	Token string     `json:"token"`
	User  *AdminUser `json:"user"`
}

// HandleLogin authenticates an admin by email and password.
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "email and password are required")
		return
	}

	if h.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database store not available")
		return
	}

	dbUser, err := h.queries.GetAdminUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "database_error", "failed to lookup user")
		return
	}

	if !CheckPassword(req.Password, dbUser.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "invalid email or password")
		return
	}

	adminUser := &AdminUser{
		ID:        dbUser.ID,
		Email:     dbUser.Email,
		Role:      Role(dbUser.Role),
		CreatedAt: dbUser.CreatedAt.Time,
		UpdatedAt: dbUser.UpdatedAt.Time,
	}

	tokenStr, err := h.tokenManager.GenerateToken(adminUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error", "failed to generate session token")
		return
	}

	h.tokenManager.SetSessionCookie(w, tokenStr, h.secureCookie)

	if h.auditor != nil {
		_ = h.auditor.Record(r.Context(), audit.Entry{
			UserID:       adminUser.ID,
			Action:       "admin_login",
			ResourceType: "admin_user",
			ResourceID:   adminUser.ID,
			Payload:      map[string]any{"email": adminUser.Email},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(LoginResponse{
		Token: tokenStr,
		User:  adminUser,
	})
}

// HandleLogout clears the admin session cookie.
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	h.tokenManager.ClearSessionCookie(w, h.secureCookie)

	if u, ok := GetAdminUser(r.Context()); ok && h.auditor != nil {
		_ = h.auditor.Record(r.Context(), audit.Entry{
			UserID:       u.ID,
			Action:       "admin_logout",
			ResourceType: "admin_user",
			ResourceID:   u.ID,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// HandleMe returns the profile of currently authenticated admin.
func (h *Handler) HandleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := GetAdminUser(r.Context())
	if !ok || u == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "admin user context not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(u)
}

// HandleListUsers lists all admin users (owner only).
func (h *Handler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database store not available")
		return
	}

	dbUsers, err := h.queries.ListAdminUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	resp := make([]*AdminUser, len(dbUsers))
	for i, u := range dbUsers {
		resp[i] = &AdminUser{
			ID:        u.ID,
			Email:     u.Email,
			Role:      Role(u.Role),
			CreatedAt: u.CreatedAt.Time,
			UpdatedAt: u.UpdatedAt.Time,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateUserRequest holds payload to create an admin user.
type CreateUserRequest struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// HandleCreateUser provisions a new admin user (owner only).
func (h *Handler) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "email and password are required")
		return
	}

	validRole, err := ValidateRole(req.Role)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	if req.ID == "" {
		sanitized := strings.ToLower(strings.ReplaceAll(req.Email, "@", "_"))
		sanitized = strings.ReplaceAll(sanitized, ".", "_")
		req.ID = fmt.Sprintf("usr_%s", sanitized)
	}

	if h.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database store not available")
		return
	}

	created, err := h.queries.CreateAdminUser(r.Context(), db.CreateAdminUserParams{
		ID:           req.ID,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         string(validRole),
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", fmt.Sprintf("failed to create admin user: %v", err))
		return
	}

	u := &AdminUser{
		ID:        created.ID,
		Email:     created.Email,
		Role:      Role(created.Role),
		CreatedAt: created.CreatedAt.Time,
		UpdatedAt: created.UpdatedAt.Time,
	}

	if current, ok := GetAdminUser(r.Context()); ok && h.auditor != nil {
		_ = h.auditor.Record(r.Context(), audit.Entry{
			UserID:       current.ID,
			Action:       "create_admin_user",
			ResourceType: "admin_user",
			ResourceID:   u.ID,
			Payload:      map[string]any{"email": u.Email, "role": string(u.Role)},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(u)
}

// HandleDeleteUser deletes an admin user (owner only, cannot delete self).
func (h *Handler) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "user id required")
		return
	}

	current, ok := GetAdminUser(r.Context())
	if ok && current != nil && current.ID == targetID {
		writeError(w, http.StatusBadRequest, "bad_request", "cannot delete own admin account")
		return
	}

	if h.queries == nil {
		writeError(w, http.StatusInternalServerError, "database_unavailable", "database store not available")
		return
	}

	if err := h.queries.DeleteAdminUser(r.Context(), targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	if ok && current != nil && h.auditor != nil {
		_ = h.auditor.Record(r.Context(), audit.Entry{
			UserID:       current.ID,
			Action:       "delete_admin_user",
			ResourceType: "admin_user",
			ResourceID:   targetID,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"id":     targetID,
	})
}

// SeedDefaultAdmin creates an initial owner user if no admin users exist.
func SeedDefaultAdmin(ctx context.Context, queries *db.Queries, email, password string) error {
	if queries == nil {
		return nil
	}
	if email == "" {
		email = "admin@beavend.local"
	}
	if password == "" {
		password = "adminpassword123"
	}

	count, err := queries.CountAdminUsers(ctx)
	if err != nil {
		return fmt.Errorf("count admin users: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash default admin password: %w", err)
	}

	_, err = queries.CreateAdminUser(ctx, db.CreateAdminUserParams{
		ID:           "usr_admin_default",
		Email:        strings.ToLower(strings.TrimSpace(email)),
		PasswordHash: hash,
		Role:         string(RoleOwner),
	})
	if err != nil {
		return fmt.Errorf("create default admin: %w", err)
	}

	slog.Info("seeded initial admin user", slog.String("email", email), slog.String("role", string(RoleOwner)))
	return nil
}
