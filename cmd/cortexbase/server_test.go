package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cortexbase/cortexbase/internal/config"
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
