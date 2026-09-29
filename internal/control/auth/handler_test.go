package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/control/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
