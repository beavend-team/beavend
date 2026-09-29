package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoles_Hierarchy(t *testing.T) {
	assert.True(t, RoleOwner.Can(RoleOwner))
	assert.True(t, RoleOwner.Can(RoleEditor))
	assert.True(t, RoleOwner.Can(RoleViewer))

	assert.False(t, RoleEditor.Can(RoleOwner))
	assert.True(t, RoleEditor.Can(RoleEditor))
	assert.True(t, RoleEditor.Can(RoleViewer))

	assert.False(t, RoleViewer.Can(RoleOwner))
	assert.False(t, RoleViewer.Can(RoleEditor))
	assert.True(t, RoleViewer.Can(RoleViewer))

	r, err := ValidateRole("owner")
	require.NoError(t, err)
	assert.Equal(t, RoleOwner, r)

	_, err = ValidateRole("superadmin")
	assert.Error(t, err)
}

func TestPassword_HashAndCheck(t *testing.T) {
	password := "supersecret123"
	hash, err := HashPassword(password)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	assert.True(t, CheckPassword(password, hash))
	assert.False(t, CheckPassword("wrongpassword", hash))

	_, err = HashPassword("short")
	assert.Error(t, err, "password under 8 characters must fail")
}

func TestTokenManager_Lifecycle(t *testing.T) {
	tm := NewTokenManager([]byte("test-secret-key-32-bytes-long!!"), 1*time.Hour)
	user := &AdminUser{
		ID:    "admin_1",
		Email: "admin@example.com",
		Role:  RoleEditor,
	}

	tokenStr, err := tm.GenerateToken(user)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenStr)

	claims, err := tm.ValidateToken(tokenStr)
	require.NoError(t, err)
	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, user.Email, claims.Email)
	assert.Equal(t, RoleEditor, claims.Role)

	// Tampered token test
	tampered := tokenStr + "tampered"
	_, err = tm.ValidateToken(tampered)
	assert.Error(t, err)

	// Expired token test
	shortTM := NewTokenManager([]byte("test-secret-key-32-bytes-long!!"), -1*time.Minute)
	expiredToken, err := shortTM.GenerateToken(user)
	require.NoError(t, err)
	_, err = tm.ValidateToken(expiredToken)
	assert.Error(t, err)
}

func TestAuthMiddleware_And_RequireRole(t *testing.T) {
	tm := NewTokenManager([]byte("test-secret-key-32-bytes-long!!"), 1*time.Hour)

	editorUser := &AdminUser{
		ID:    "usr_editor",
		Email: "editor@example.com",
		Role:  RoleEditor,
	}
	editorToken, err := tm.GenerateToken(editorUser)
	require.NoError(t, err)

	viewerUser := &AdminUser{
		ID:    "usr_viewer",
		Email: "viewer@example.com",
		Role:  RoleViewer,
	}
	viewerToken, err := tm.GenerateToken(viewerUser)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := GetAdminUser(r.Context())
		if !ok {
			http.Error(w, "no user", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok:" + u.Email))
	})

	securedEditorEndpoint := Middleware(tm)(RequireRole(RoleEditor)(handler))

	// 1. No auth -> 401
	req1 := httptest.NewRequest("GET", "/admin/test", nil)
	rec1 := httptest.NewRecorder()
	securedEditorEndpoint.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusUnauthorized, rec1.Code)

	// 2. Viewer token on editor endpoint -> 403
	req2 := httptest.NewRequest("GET", "/admin/test", nil)
	req2.Header.Set("Authorization", "Bearer "+viewerToken)
	rec2 := httptest.NewRecorder()
	securedEditorEndpoint.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusForbidden, rec2.Code)

	// 3. Editor token on editor endpoint (Bearer header) -> 200
	req3 := httptest.NewRequest("GET", "/admin/test", nil)
	req3.Header.Set("Authorization", "Bearer "+editorToken)
	rec3 := httptest.NewRecorder()
	securedEditorEndpoint.ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusOK, rec3.Code)
	assert.Contains(t, rec3.Body.String(), "ok:editor@example.com")

	// 4. Editor token via Cookie -> 200
	req4 := httptest.NewRequest("GET", "/admin/test", nil)
	req4.AddCookie(&http.Cookie{
		Name:  CookieSessionName,
		Value: editorToken,
	})
	rec4 := httptest.NewRecorder()
	securedEditorEndpoint.ServeHTTP(rec4, req4)
	assert.Equal(t, http.StatusOK, rec4.Code)
}
