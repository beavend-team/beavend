package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
	pgx "github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockUserStore struct {
	users map[string]db.AdminUsers
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{users: make(map[string]db.AdminUsers)}
}

func (m *mockUserStore) GetAdminUserByEmail(_ context.Context, email string) (db.AdminUsers, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return db.AdminUsers{}, pgx.ErrNoRows
}

func (m *mockUserStore) GetAdminUserByID(_ context.Context, id string) (db.AdminUsers, error) {
	u, ok := m.users[id]
	if !ok {
		return db.AdminUsers{}, pgx.ErrNoRows
	}
	return u, nil
}

func (m *mockUserStore) ListAdminUsers(_ context.Context) ([]db.AdminUsers, error) {
	var list []db.AdminUsers
	for _, u := range m.users {
		list = append(list, u)
	}
	return list, nil
}

func (m *mockUserStore) CreateAdminUser(_ context.Context, arg db.CreateAdminUserParams) (db.AdminUsers, error) {
	u := db.AdminUsers{
		ID:           arg.ID,
		Email:        arg.Email,
		PasswordHash: arg.PasswordHash,
		Role:         arg.Role,
	}
	m.users[arg.ID] = u
	return u, nil
}

func (m *mockUserStore) DeleteAdminUser(_ context.Context, id string) error {
	delete(m.users, id)
	return nil
}

func (m *mockUserStore) CountAdminUsers(_ context.Context) (int64, error) {
	return int64(len(m.users)), nil
}

func (m *mockUserStore) CountOwnerAdminUsers(_ context.Context) (int64, error) {
	var count int64
	for _, u := range m.users {
		if u.Role == string(RoleOwner) && !u.IsDisabled {
			count++
		}
	}
	return count, nil
}

func (m *mockUserStore) UpdateAdminUserDisabled(_ context.Context, arg db.UpdateAdminUserDisabledParams) (db.AdminUsers, error) {
	u, ok := m.users[arg.ID]
	if !ok {
		return db.AdminUsers{}, pgx.ErrNoRows
	}
	u.IsDisabled = arg.IsDisabled
	m.users[arg.ID] = u
	return u, nil
}

func TestHandler_LoginValidationAndMe(t *testing.T) {
	tm := NewTokenManager([]byte("test-key-32-chars-long-security!"), 1*time.Hour)
	auditor := audit.NewInMemoryAuditor()
	handler := NewHandler(nil, tm, auditor, false)

	// Test 1: Empty credentials
	body, _ := json.Marshal(LoginRequest{Email: "", Password: ""})
	req := httptest.NewRequest("POST", "/v1/admin/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleLogin(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Test 2: Me without auth context
	reqMe := httptest.NewRequest("GET", "/v1/admin/auth/me", nil)
	recMe := httptest.NewRecorder()
	handler.HandleMe(recMe, reqMe)
	assert.Equal(t, http.StatusUnauthorized, recMe.Code)

	// Test 3: Me with auth context
	user := &AdminUser{
		ID:        "usr_1",
		Email:     "admin@example.com",
		Role:      RoleOwner,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	reqMeAuthed := httptest.NewRequest("GET", "/v1/admin/auth/me", nil)
	reqMeAuthed = reqMeAuthed.WithContext(WithAdminUser(reqMeAuthed.Context(), user))
	recMeAuthed := httptest.NewRecorder()
	handler.HandleMe(recMeAuthed, reqMeAuthed)
	assert.Equal(t, http.StatusOK, recMeAuthed.Code)

	var meResp AdminUser
	err := json.NewDecoder(recMeAuthed.Body).Decode(&meResp)
	require.NoError(t, err)
	assert.Equal(t, user.ID, meResp.ID)
	assert.Equal(t, user.Email, meResp.Email)
	assert.Equal(t, RoleOwner, meResp.Role)

	// Test 4: Logout
	reqLogout := httptest.NewRequest("POST", "/v1/admin/auth/logout", nil)
	reqLogout = reqLogout.WithContext(WithAdminUser(reqLogout.Context(), user))
	recLogout := httptest.NewRecorder()
	handler.HandleLogout(recLogout, reqLogout)
	assert.Equal(t, http.StatusOK, recLogout.Code)

	// Verify logout audit entry was recorded
	entries := auditor.Entries()
	require.NotEmpty(t, entries)
	assert.Equal(t, "admin_logout", entries[len(entries)-1].Action)
}

func TestHandler_SecurityHardening(t *testing.T) {
	tm := NewTokenManager([]byte("test-key-32-chars-long-security!"), 1*time.Hour)
	auditor := audit.NewInMemoryAuditor()
	store := newMockUserStore()

	hash1, err := HashPassword("validpass123")
	require.NoError(t, err)
	store.users["usr_active_owner"] = db.AdminUsers{
		ID:           "usr_active_owner",
		Email:        "owner@example.com",
		PasswordHash: hash1,
		Role:         string(RoleOwner),
		IsDisabled:   false,
	}

	hash2, err := HashPassword("validpass123")
	require.NoError(t, err)
	store.users["usr_disabled"] = db.AdminUsers{
		ID:           "usr_disabled",
		Email:        "disabled@example.com",
		PasswordHash: hash2,
		Role:         string(RoleEditor),
		IsDisabled:   true,
	}

	handler := NewHandler(store, tm, auditor, false)

	t.Run("successful login with active credentials", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{Email: "owner@example.com", Password: "validpass123"})
		req := httptest.NewRequest("POST", "/v1/admin/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.HandleLogin(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp LoginResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.NotEmpty(t, resp.Token)
		assert.Equal(t, "owner@example.com", resp.User.Email)
	})

	t.Run("login fails for disabled user with 403 Forbidden", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{Email: "disabled@example.com", Password: "validpass123"})
		req := httptest.NewRequest("POST", "/v1/admin/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.HandleLogin(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "admin account is disabled")
	})

	t.Run("login fails with wrong password", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{Email: "owner@example.com", Password: "wrongpassword"})
		req := httptest.NewRequest("POST", "/v1/admin/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.HandleLogin(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("login with non-existent email runs constant-time dummy check and returns 401", func(t *testing.T) {
		body, _ := json.Marshal(LoginRequest{Email: "ghost@example.com", Password: "somepassword"})
		req := httptest.NewRequest("POST", "/v1/admin/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.HandleLogin(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("last owner deletion protection returns 409 Conflict", func(t *testing.T) {
		// Currently only 1 owner ("usr_active_owner")
		// Simulate another owner calling delete on usr_active_owner
		r := chi.NewRouter()
		r.Delete("/v1/admin/users/{id}", handler.HandleDeleteUser)

		req := httptest.NewRequest("DELETE", "/v1/admin/users/usr_active_owner", nil)
		caller := &AdminUser{ID: "usr_other_caller", Role: RoleOwner}
		req = req.WithContext(WithAdminUser(req.Context(), caller))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "cannot delete the last admin owner account")
	})

	t.Run("owner deletion succeeds when multiple owners exist", func(t *testing.T) {
		// Add a second owner
		store.users["usr_second_owner"] = db.AdminUsers{
			ID:         "usr_second_owner",
			Email:      "second_owner@example.com",
			Role:       string(RoleOwner),
			IsDisabled: false,
		}

		r := chi.NewRouter()
		r.Delete("/v1/admin/users/{id}", handler.HandleDeleteUser)

		req := httptest.NewRequest("DELETE", "/v1/admin/users/usr_second_owner", nil)
		caller := &AdminUser{ID: "usr_active_owner", Role: RoleOwner}
		req = req.WithContext(WithAdminUser(req.Context(), caller))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "deleted")
	})
}
