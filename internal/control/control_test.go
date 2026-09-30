package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/control/auth"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPublisher struct {
	events []synckit.RevisionEvent
}

func (m *mockPublisher) Publish(_ context.Context, event synckit.RevisionEvent) error {
	m.events = append(m.events, event)
	return nil
}

func TestControlService_InvalidateSnapshot(t *testing.T) {
	pub := &mockPublisher{}
	svc := NewService(nil, pub, nil, []byte("test-key-32-chars-long-security!"))
	rev, err := svc.InvalidateSnapshot(context.Background(), "proj_1", "development")
	require.NoError(t, err)
	assert.Equal(t, int64(0), rev)
}

func TestControlService_RecordAudit(t *testing.T) {
	auditor := audit.NewInMemoryAuditor()
	svc := NewService(nil, nil, auditor, []byte("test-key-32-chars-long-security!"))

	user := &auth.AdminUser{
		ID:    "usr_admin",
		Email: "admin@beavend.local",
		Role:  auth.RoleOwner,
	}
	ctx := auth.WithAdminUser(context.Background(), user)

	svc.RecordAudit(ctx, "proj_1", "production", "create_app", "ai_app", "app_1", map[string]string{"foo": "bar"})

	entries := auditor.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "proj_1", entries[0].ProjectID)
	assert.Equal(t, "production", entries[0].Environment)
	assert.Equal(t, "usr_admin", entries[0].UserID)
	assert.Equal(t, "create_app", entries[0].Action)
}

func TestHandler_CELPolicyValidation(t *testing.T) {
	auditor := audit.NewInMemoryAuditor()
	svc := NewService(nil, nil, auditor, []byte("test-key-32-chars-long-security!"))
	handler := NewHandler(svc)

	// Invalid CEL expression should fail validation
	badReq := CreatePolicyRequest{
		ID:         "pol_invalid",
		Name:       "Bad Policy",
		Expression: "claims.unknown_func(>>>) == invalid syntax",
	}
	body, _ := json.Marshal(badReq)
	req := httptest.NewRequest("POST", "/v1/admin/policies", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleCreatePolicy(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_cel_expression")

	// Missing expression should return 400
	emptyReq := CreatePolicyRequest{
		ID:   "pol_empty",
		Name: "Empty",
	}
	bodyEmpty, _ := json.Marshal(emptyReq)
	reqEmpty := httptest.NewRequest("POST", "/v1/admin/policies", bytes.NewReader(bodyEmpty))
	recEmpty := httptest.NewRecorder()
	handler.HandleCreatePolicy(recEmpty, reqEmpty)
	assert.Equal(t, http.StatusBadRequest, recEmpty.Code)
}

func TestHandler_ToolJSONSchemaValidation(t *testing.T) {
	auditor := audit.NewInMemoryAuditor()
	svc := NewService(nil, nil, auditor, []byte("test-key-32-chars-long-security!"))
	handler := NewHandler(svc)

	// Invalid JSON Schema (e.g. invalid type) should fail validation
	badReq := CreateToolRequest{
		ID:          "tool_bad",
		Name:        "Bad Tool",
		InputSchema: json.RawMessage(`{"type": "nonexistent_type"}`),
	}
	body, _ := json.Marshal(badReq)
	req := httptest.NewRequest("POST", "/v1/admin/tools", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleCreateTool(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_schema")

	// Mutating SQL template must fail validation
	badSQLReq := CreateToolRequest{
		ID:       "tool_bad_sql",
		Name:     "Bad SQL Tool",
		Executor: json.RawMessage(`{"type": "sql_template", "template": "DROP TABLE users;"}`),
	}
	bodySQL, _ := json.Marshal(badSQLReq)
	reqSQL := httptest.NewRequest("POST", "/v1/admin/tools", bytes.NewReader(bodySQL))
	recSQL := httptest.NewRecorder()
	handler.HandleCreateTool(recSQL, reqSQL)
	assert.Equal(t, http.StatusBadRequest, recSQL.Code)
	assert.Contains(t, recSQL.Body.String(), "invalid_sql_template")

	// Empty MCP URL must fail validation
	badMCPReq := CreateToolRequest{
		ID:       "tool_bad_mcp",
		Name:     "Bad MCP Tool",
		Executor: json.RawMessage(`{"type": "mcp", "url": "  "}`),
	}
	bodyMCP, _ := json.Marshal(badMCPReq)
	reqMCP := httptest.NewRequest("POST", "/v1/admin/tools", bytes.NewReader(bodyMCP))
	recMCP := httptest.NewRecorder()
	handler.HandleCreateTool(recMCP, reqMCP)
	assert.Equal(t, http.StatusBadRequest, recMCP.Code)
	assert.Contains(t, recMCP.Body.String(), "invalid_executor")
}

func TestMountRoutes_RBAC(t *testing.T) {
	tm := auth.NewTokenManager([]byte("test-key-32-chars-long-security!"), 1*time.Hour)
	auditor := audit.NewInMemoryAuditor()
	svc := NewService(nil, nil, auditor, []byte("test-key-32-chars-long-security!"))
	ctrlHandler := NewHandler(svc)
	authHandler := auth.NewHandler(nil, tm, auditor, false)

	r := chi.NewRouter()
	r.Route("/v1/admin", func(sub chi.Router) {
		MountRoutes(sub, authHandler, ctrlHandler, tm)
	})

	viewerUser := &auth.AdminUser{ID: "usr_v", Email: "viewer@test.com", Role: auth.RoleViewer}
	editorUser := &auth.AdminUser{ID: "usr_e", Email: "editor@test.com", Role: auth.RoleEditor}
	ownerUser := &auth.AdminUser{ID: "usr_o", Email: "owner@test.com", Role: auth.RoleOwner}

	viewerToken, _ := tm.GenerateToken(viewerUser)
	editorToken, _ := tm.GenerateToken(editorUser)
	ownerToken, _ := tm.GenerateToken(ownerUser)

	// 1. Health check is public
	reqHealth := httptest.NewRequest("GET", "/v1/admin/health", nil)
	recHealth := httptest.NewRecorder()
	r.ServeHTTP(recHealth, reqHealth)
	assert.Equal(t, http.StatusOK, recHealth.Code)

	// 2. Unauthenticated request to /v1/admin/apps returns 401
	reqUnauth := httptest.NewRequest("GET", "/v1/admin/apps", nil)
	recUnauth := httptest.NewRecorder()
	r.ServeHTTP(recUnauth, reqUnauth)
	assert.Equal(t, http.StatusUnauthorized, recUnauth.Code)

	// 3. Viewer can GET /v1/admin/apps (even if DB is nil, it gets past auth to handler which returns 500 database_unavailable)
	reqViewerGet := httptest.NewRequest("GET", "/v1/admin/apps", nil)
	reqViewerGet.Header.Set("Authorization", "Bearer "+viewerToken)
	recViewerGet := httptest.NewRecorder()
	r.ServeHTTP(recViewerGet, reqViewerGet)
	assert.NotEqual(t, http.StatusUnauthorized, recViewerGet.Code)
	assert.NotEqual(t, http.StatusForbidden, recViewerGet.Code)

	// 4. Viewer cannot POST to /v1/admin/apps (returns 403 Forbidden)
	reqViewerPost := httptest.NewRequest("POST", "/v1/admin/apps", bytes.NewReader([]byte("{}")))
	reqViewerPost.Header.Set("Authorization", "Bearer "+viewerToken)
	recViewerPost := httptest.NewRecorder()
	r.ServeHTTP(recViewerPost, reqViewerPost)
	assert.Equal(t, http.StatusForbidden, recViewerPost.Code)

	// 5. Editor cannot access Owner-only user list (returns 403 Forbidden)
	reqEditorUsers := httptest.NewRequest("GET", "/v1/admin/users", nil)
	reqEditorUsers.Header.Set("Authorization", "Bearer "+editorToken)
	recEditorUsers := httptest.NewRecorder()
	r.ServeHTTP(recEditorUsers, reqEditorUsers)
	assert.Equal(t, http.StatusForbidden, recEditorUsers.Code)

	// 6. Owner can access Owner-only user list
	reqOwnerUsers := httptest.NewRequest("GET", "/v1/admin/users", nil)
	reqOwnerUsers.Header.Set("Authorization", "Bearer "+ownerToken)
	recOwnerUsers := httptest.NewRecorder()
	r.ServeHTTP(recOwnerUsers, reqOwnerUsers)
	assert.NotEqual(t, http.StatusForbidden, recOwnerUsers.Code)
	assert.NotEqual(t, http.StatusUnauthorized, recOwnerUsers.Code)
}

func TestControlService_PurgeSubjectCache(t *testing.T) {
	cacheStore := cache.NewMemoryCacheStore()
	svc := NewService(nil, nil, nil, []byte("test-key-32-chars-long-security!"))
	svc.SetCacheStore(cacheStore)

	ctx := context.Background()
	_ = cacheStore.Set(ctx, "rc:proj_1:prod:user:alice:hash1", []byte("resp1"), time.Minute)
	_ = cacheStore.Set(ctx, "tc:proj_1:prod:alice:tool1:argshash1", []byte("tool1"), time.Minute)
	_ = cacheStore.Set(ctx, "rc:proj_1:prod:user:bob:hash2", []byte("resp2"), time.Minute)

	err := svc.PurgeSubjectCache(ctx, "proj_1", "prod", "alice")
	require.NoError(t, err)

	val1, _ := cacheStore.Get(ctx, "rc:proj_1:prod:user:alice:hash1")
	assert.Nil(t, val1, "alice response cache should be purged")

	val2, _ := cacheStore.Get(ctx, "tc:proj_1:prod:alice:tool1:argshash1")
	assert.Nil(t, val2, "alice tool cache should be purged")

	val3, _ := cacheStore.Get(ctx, "rc:proj_1:prod:user:bob:hash2")
	assert.Equal(t, []byte("resp2"), val3, "bob cache should remain intact")
}

func TestHandler_TestToken(t *testing.T) {
	svc := NewService(nil, nil, nil, []byte("test-key-32-chars-long-security!"))
	h := NewHandler(svc)

	// 1. Empty token
	req1 := httptest.NewRequest("POST", "/v1/admin/auth/test-token", bytes.NewReader([]byte(`{"token":""}`)))
	rec1 := httptest.NewRecorder()
	h.HandleTestToken(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)
	var resp1 TestTokenResponse
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	assert.False(t, resp1.Valid)
	assert.Contains(t, resp1.Error, "token is required")

	// 2. Token without auth config
	req2 := httptest.NewRequest("POST", "/v1/admin/auth/test-token", bytes.NewReader([]byte(`{"token":"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjMifQ.sig"}`)))
	rec2 := httptest.NewRecorder()
	h.HandleTestToken(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	var resp2 TestTokenResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.False(t, resp2.Valid)
	assert.Contains(t, resp2.Error, "no auth configuration")
}
