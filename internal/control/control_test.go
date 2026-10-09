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
	"github.com/beavend/beavend/internal/store/db"
	chi "github.com/go-chi/chi/v5"
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

func TestHandler_HandleTestPolicy(t *testing.T) {
	svc := NewService(nil, nil, nil, []byte("test-key-32-chars-long-security!"))
	h := NewHandler(svc)

	// 1. Empty expression
	req1 := httptest.NewRequest("POST", "/v1/admin/policies/test", bytes.NewReader([]byte(`{"expression":""}`)))
	rec1 := httptest.NewRecorder()
	h.HandleTestPolicy(rec1, req1)
	assert.Equal(t, http.StatusBadRequest, rec1.Code)

	// 2. Syntax error
	req2 := httptest.NewRequest("POST", "/v1/admin/policies/test", bytes.NewReader([]byte(`{"expression":"claims.invalid >>> 123"}`)))
	rec2 := httptest.NewRecorder()
	h.HandleTestPolicy(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	var resp2 TestPolicyResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.False(t, resp2.Valid)
	assert.NotEmpty(t, resp2.Error)

	// 3. Valid expression evaluating to true
	body3 := `{"expression":"claims.plan == 'pro' && claims.email_verified == true", "claims":{"plan":"pro","email_verified":true}}`
	req3 := httptest.NewRequest("POST", "/v1/admin/policies/test", bytes.NewReader([]byte(body3)))
	rec3 := httptest.NewRecorder()
	h.HandleTestPolicy(rec3, req3)
	assert.Equal(t, http.StatusOK, rec3.Code)
	var resp3 TestPolicyResponse
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &resp3))
	assert.True(t, resp3.Valid)
	assert.True(t, resp3.Allowed)
	assert.Empty(t, resp3.Error)

	// 4. Valid expression evaluating to false
	body4 := `{"expression":"claims.plan == 'enterprise'", "claims":{"plan":"pro"}}`
	req4 := httptest.NewRequest("POST", "/v1/admin/policies/test", bytes.NewReader([]byte(body4)))
	rec4 := httptest.NewRecorder()
	h.HandleTestPolicy(rec4, req4)
	assert.Equal(t, http.StatusOK, rec4.Code)
	var resp4 TestPolicyResponse
	require.NoError(t, json.Unmarshal(rec4.Body.Bytes(), &resp4))
	assert.True(t, resp4.Valid)
	assert.False(t, resp4.Allowed)
	assert.Empty(t, resp4.Error)
}

func TestParseModelList(t *testing.T) {
	// 1. OpenAI / LiteLLM v1/models format
	openAIResp := []byte(`{"object":"list","data":[{"id":"gpt-4o","object":"model"},{"id":"gpt-4o-mini","object":"model"}]}`)
	models1 := parseModelList(openAIResp)
	assert.Equal(t, []string{"gpt-4o", "gpt-4o-mini"}, models1)

	// 2. LiteLLM /models string array format
	liteLLMResp := []byte(`{"data":["claude-3-5-sonnet","gpt-4o"]}`)
	models2 := parseModelList(liteLLMResp)
	assert.Equal(t, []string{"claude-3-5-sonnet", "gpt-4o"}, models2)

	// 3. Ollama /api/tags format
	ollamaResp := []byte(`{"models":[{"name":"llama3:latest"},{"model":"qwen2.5:latest"}]}`)
	models3 := parseModelList(ollamaResp)
	assert.Equal(t, []string{"llama3:latest", "qwen2.5:latest"}, models3)

	// 4. Direct JSON list
	listResp := []byte(`[{"id":"deepseek-r1"},{"id":"deepseek-v3"}]`)
	models4 := parseModelList(listResp)
	assert.Equal(t, []string{"deepseek-r1", "deepseek-v3"}, models4)
}

func TestHandler_AppAuthValidation(t *testing.T) {
	auditor := audit.NewInMemoryAuditor()
	svc := NewService(nil, nil, auditor, []byte("test-key-32-chars-long-security!"))
	handler := NewHandler(svc)

	t.Run("invalid auth provider returns 400", func(t *testing.T) {
		reqBody := map[string]any{
			"id":   "app_test_bad_auth",
			"name": "Bad Auth App",
			"auth": map[string]any{
				"provider": "unsupported_provider",
			},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/v1/admin/apps", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.HandleCreateAIApp(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "unsupported auth provider")
	})

	t.Run("valid auth passes validation and checks db", func(t *testing.T) {
		reqBody := map[string]any{
			"id":   "app_test_valid",
			"name": "Valid App",
			"auth": map[string]any{
				"provider": "anonymous",
			},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/v1/admin/apps", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		handler.HandleCreateAIApp(rec, req)
		// Validation passes, then encounters nil queries
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "database_unavailable")
	})
}

func TestToAIAppResponse_JSONSerialization(t *testing.T) {
	app := db.AiApps{
		ID:          "app_test_supabase",
		ProjectID:   "proj_default",
		Environment: "development",
		Name:        "Test Supabase App",
		AuthConfig:  []byte(`{"provider":"supabase","issuer":"https://xyz.supabase.co/auth/v1","audience":["authenticated"]}`),
		ModelRoutes: []byte(`{"default":"gpt-4o"}`),
	}

	resp := toAIAppResponse(app)
	marshaled, err := json.Marshal(resp)
	assert.NoError(t, err)

	// Verify that the output JSON contains nested objects and NOT base64 strings
	var decoded map[string]any
	err = json.Unmarshal(marshaled, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "app_test_supabase", decoded["id"])
	authMap, ok := decoded["auth_config"].(map[string]any)
	assert.True(t, ok, "auth_config must serialize to a JSON object, not base64 string")
	assert.Equal(t, "supabase", authMap["provider"])
	assert.Equal(t, "https://xyz.supabase.co/auth/v1", authMap["issuer"])

	routesMap, ok := decoded["model_routes"].(map[string]any)
	assert.True(t, ok, "model_routes must serialize to a JSON object, not base64 string")
	assert.Equal(t, "gpt-4o", routesMap["default"])
}



