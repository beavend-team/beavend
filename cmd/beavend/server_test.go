package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/config"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/prompts"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime"
	"github.com/beavend/beavend/internal/runtime/stages"
	"github.com/beavend/beavend/internal/snapshot"
	"github.com/beavend/beavend/internal/tools"
)

type mockReadyChecker struct {
	ready   bool
	details map[string]any
}

func (m *mockReadyChecker) IsReady(_ context.Context) (bool, map[string]any) {
	return m.ready, m.details
}

func testConfig(role string) *config.Config {
	return &config.Config{
		Env:  config.EnvDevelopment,
		Role: role,
		HTTP: config.HTTPConfig{
			Port:         8080,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
}

func TestHealthzProbe(t *testing.T) {
	cfg := testConfig(config.RoleAll)
	router := BuildRouter(cfg, nil, &DefaultReadyChecker{Role: "all"})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
}

func TestReadyzProbe(t *testing.T) {
	cfg := testConfig(config.RoleAll)

	// When ready
	readyChecker := &mockReadyChecker{
		ready:   true,
		details: map[string]any{"status": "ready", "revision": 1},
	}
	router := BuildRouter(cfg, nil, readyChecker)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// When unready
	unreadyChecker := &mockReadyChecker{
		ready:   false,
		details: map[string]any{"status": "not_ready", "reason": "snapshot compiling"},
	}
	routerUnready := BuildRouter(cfg, nil, unreadyChecker)

	reqUnready := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recUnready := httptest.NewRecorder()
	routerUnready.ServeHTTP(recUnready, reqUnready)

	if recUnready.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", recUnready.Code)
	}
}

func TestRoleRouteSeparation(t *testing.T) {
	// 1. Role: runtime
	cfgRuntime := testConfig(config.RoleRuntime)
	routerRuntime := BuildRouter(cfgRuntime, nil, &DefaultReadyChecker{Role: config.RoleRuntime})

	// Runtime route should exist
	reqApp := httptest.NewRequest(http.MethodGet, "/v1/apps/assistant", nil)
	recApp := httptest.NewRecorder()
	routerRuntime.ServeHTTP(recApp, reqApp)
	if recApp.Code != http.StatusOK {
		t.Errorf("runtime role expected 200 for /v1/apps/assistant, got %d", recApp.Code)
	}

	// Admin route should NOT exist (404)
	reqAdmin := httptest.NewRequest(http.MethodGet, "/v1/admin/health", nil)
	recAdmin := httptest.NewRecorder()
	routerRuntime.ServeHTTP(recAdmin, reqAdmin)
	if recAdmin.Code != http.StatusNotFound {
		t.Errorf("runtime role expected 404 for /v1/admin/health, got %d", recAdmin.Code)
	}

	// 2. Role: control
	cfgControl := testConfig(config.RoleControl)
	routerControl := BuildRouter(cfgControl, nil, &DefaultReadyChecker{Role: config.RoleControl})

	// Runtime route should NOT exist (404)
	recAppCtrl := httptest.NewRecorder()
	routerControl.ServeHTTP(recAppCtrl, reqApp)
	if recAppCtrl.Code != http.StatusNotFound {
		t.Errorf("control role expected 404 for /v1/apps/assistant, got %d", recAppCtrl.Code)
	}

	// Admin route should exist
	recAdminCtrl := httptest.NewRecorder()
	routerControl.ServeHTTP(recAdminCtrl, reqAdmin)
	if recAdminCtrl.Code != http.StatusOK {
		t.Errorf("control role expected 200 for /v1/admin/health, got %d", recAdminCtrl.Code)
	}

	// 3. Role: all
	cfgAll := testConfig(config.RoleAll)
	routerAll := BuildRouter(cfgAll, nil, &DefaultReadyChecker{Role: config.RoleAll})

	recAppAll := httptest.NewRecorder()
	routerAll.ServeHTTP(recAppAll, reqApp)
	if recAppAll.Code != http.StatusOK {
		t.Errorf("all role expected 200 for /v1/apps/assistant, got %d", recAppAll.Code)
	}

	recAdminAll := httptest.NewRecorder()
	routerAll.ServeHTTP(recAdminAll, reqAdmin)
	if recAdminAll.Code != http.StatusOK {
		t.Errorf("all role expected 200 for /v1/admin/health, got %d", recAdminAll.Code)
	}
}

func TestRequestIDHeaderPropagation(t *testing.T) {
	cfg := testConfig(config.RoleAll)
	router := BuildRouter(cfg, nil, &DefaultReadyChecker{Role: config.RoleAll})

	// 1. With client-supplied X-Request-Id
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "client_req_999")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "client_req_999" {
		t.Errorf("expected X-Request-Id 'client_req_999', got %q", got)
	}

	// 2. Without client-supplied X-Request-Id (server generated)
	reqAuto := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recAuto := httptest.NewRecorder()
	router.ServeHTTP(recAuto, reqAuto)

	if got := recAuto.Header().Get("X-Request-Id"); got == "" {
		t.Errorf("expected server-generated X-Request-Id header in response, got empty")
	}
}

type mockServerAdapter struct{}

func (m *mockServerAdapter) Chat(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{
		ID: "chat_resp_1",
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: "Hello from server!",
		},
		Usage: core.UsageStats{TotalTokens: 42},
	}, nil
}

func (m *mockServerAdapter) Stream(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
	ch := make(chan core.StreamEvent, 3)
	go func() {
		ch <- core.StreamEvent{Type: "delta", Delta: "Streaming token"}
		ch <- core.StreamEvent{Type: "done"}
		close(ch)
	}()
	return ch, nil
}

func (m *mockServerAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return nil, nil
}

func (m *mockServerAdapter) Health(_ context.Context) error {
	return nil
}

func TestRuntimeEndpointsWiring(t *testing.T) {
	cfg := testConfig(config.RoleAll)

	registry := snapshot.NewRegistry()
	snap := core.NewSnapshot("proj_test", "development", 1)
	snap.Apps["assistant"] = &core.AIApp{
		ID:             "assistant",
		ModelRoutingID: "fast",
	}
	snap.Models["fast"] = &core.ModelRouting{
		ID:           "mr_fast",
		LogicalModel: "fast",
		Deployment:   "gpt-4o",
		Adapter:      "litellm",
	}
	registry.Swap(snap)

	modelResolver := routing.NewResolver()
	modelResolver.RegisterAdapter("litellm", &mockServerAdapter{})

	promptStage, _ := stages.NewPromptStage(prompts.NewEngine())
	routingStage, _ := stages.NewModelRoutingStage(modelResolver)
	execStage, _ := stages.NewExecutionStage(modelResolver)
	pipeline := runtime.NewStandardPipeline(nil, nil, promptStage, routingStage, execStage)

	runtimeHandler := runtime.NewHandler(registry, pipeline)
	router := BuildRouter(cfg, nil, &DefaultReadyChecker{Role: config.RoleAll}, RouterOptions{
		RuntimeHandler: runtimeHandler,
	})

	// 1. Unary Chat
	chatBody := `{"messages":[{"role":"user","content":"Hello"}]}`
	reqChat := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/chat", bytes.NewBufferString(chatBody))
	reqChat.Header.Set("X-Project-ID", "proj_test")
	reqChat.Header.Set("X-Environment", "development")
	recChat := httptest.NewRecorder()

	router.ServeHTTP(recChat, reqChat)

	if recChat.Code != http.StatusOK {
		t.Fatalf("expected 200 for chat completion, got %d: %s", recChat.Code, recChat.Body.String())
	}

	var chatResp core.ChatResponse
	if err := json.Unmarshal(recChat.Body.Bytes(), &chatResp); err != nil {
		t.Fatalf("failed to decode chat response: %v", err)
	}
	if chatResp.Message.Content != "Hello from server!" {
		t.Errorf("unexpected content: %q", chatResp.Message.Content)
	}
	if chatResp.Usage.TotalTokens != 42 {
		t.Errorf("expected 42 tokens, got %d", chatResp.Usage.TotalTokens)
	}

	// 2. Stream
	streamBody := `{"messages":[{"role":"user","content":"Stream me"}]}`
	reqStream := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/stream", bytes.NewBufferString(streamBody))
	reqStream.Header.Set("X-Project-ID", "proj_test")
	reqStream.Header.Set("X-Environment", "development")
	recStream := httptest.NewRecorder()

	router.ServeHTTP(recStream, reqStream)

	if recStream.Code != http.StatusOK {
		t.Fatalf("expected 200 for stream completion, got %d: %s", recStream.Code, recStream.Body.String())
	}
	if !strings.Contains(recStream.Body.String(), "event: delta\ndata: {\"text\":\"Streaming token\"}\n\n") {
		t.Errorf("expected delta event chunk in body: %s", recStream.Body.String())
	}

	// 3. Confirm
	suspState := &tools.SuspendedState{
		ProjectID:   "proj_test",
		Environment: "development",
		RequestID:   "req_confirm_svr",
		AIAppID:     "assistant",
		Subject:     "user_test",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Run tool"},
		},
		PendingToolCall: core.ToolCall{
			ID:        "t_act",
			Name:      "test_action",
			Arguments: `{}`,
		},
	}
	_ = runtimeHandler.ContinuationManager().Store().Save(context.Background(), suspState, 5*time.Minute)
	confirmToken, _ := runtimeHandler.ContinuationManager().IssueConfirmationToken(suspState, suspState.PendingToolCall, 5*time.Minute)

	confirmBody := `{"confirmation_token":"` + confirmToken + `","approved":false}`
	reqConfirm := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/confirm", bytes.NewBufferString(confirmBody))
	reqConfirm.Header.Set("X-Project-ID", "proj_test")
	reqConfirm.Header.Set("X-Environment", "development")
	reqConfirm = reqConfirm.WithContext(identity.ContextWithIdentity(reqConfirm.Context(), &core.Identity{Subject: "user_test", Project: "proj_test"}))
	recConfirm := httptest.NewRecorder()

	router.ServeHTTP(recConfirm, reqConfirm)
	if recConfirm.Code != http.StatusOK {
		t.Fatalf("expected 200 for confirm, got %d: %s", recConfirm.Code, recConfirm.Body.String())
	}

	// 4. Tool Results
	suspStateClient := &tools.SuspendedState{
		ProjectID:   "proj_test",
		Environment: "development",
		RequestID:   "req_client_svr",
		AIAppID:     "assistant",
		Subject:     "user_client",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Take photo"},
		},
		PendingToolCall: core.ToolCall{
			ID:        "t_cam",
			Name:      "camera",
			Arguments: `{}`,
		},
	}
	_ = runtimeHandler.ContinuationManager().Store().Save(context.Background(), suspStateClient, 5*time.Minute)
	clientToken, _ := runtimeHandler.ContinuationManager().IssueConfirmationToken(suspStateClient, suspStateClient.PendingToolCall, 5*time.Minute)

	toolResultsBody := `{"continuation_token":"` + clientToken + `","call_id":"t_cam","result":{"photo":"ok"}}`
	reqToolResults := httptest.NewRequest(http.MethodPost, "/v1/apps/assistant/tool-results", bytes.NewBufferString(toolResultsBody))
	reqToolResults.Header.Set("X-Project-ID", "proj_test")
	reqToolResults.Header.Set("X-Environment", "development")
	reqToolResults = reqToolResults.WithContext(identity.ContextWithIdentity(reqToolResults.Context(), &core.Identity{Subject: "user_client", Project: "proj_test"}))
	recToolResults := httptest.NewRecorder()

	router.ServeHTTP(recToolResults, reqToolResults)
	if recToolResults.Code != http.StatusOK {
		t.Fatalf("expected 200 for tool-results, got %d: %s", recToolResults.Code, recToolResults.Body.String())
	}
}
