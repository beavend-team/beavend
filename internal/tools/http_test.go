package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
)

func TestHTTPExecutor_GET_WithTemplatingAndForwardToken(t *testing.T) {
	var receivedAuth string
	var receivedPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"orders":[{"id":"ord_1","status":"delivered"}]}`))
	}))
	defer ts.Close()

	client := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: true}, 5*time.Second)
	auth := NewDownstreamAuthenticator(nil)

	def := core.ToolDefinition{
		ID:   "get_orders",
		Name: "get_orders",
		Executor: core.ExecutorConfig{
			Type:   "http",
			Method: "GET",
			URL:    ts.URL + "/users/{user_id}/orders",
		},
		DownstreamAuth: core.DownstreamAuthConfig{
			Mode: "forward_user_token",
		},
	}

	exec := NewHTTPExecutor(def, client, auth)

	id := core.Identity{
		Project: "proj_1",
		Subject: "usr_alice",
	}

	ctx := identity.ContextWithRawToken(context.Background(), "mock.jwt.token")

	call := core.ToolCall{
		ID:        "call_1",
		Name:      "get_orders",
		Arguments: `{"limit":5}`,
	}

	res, err := exec.Execute(ctx, call, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Content)
	}

	if receivedAuth != "Bearer mock.jwt.token" {
		t.Errorf("expected Bearer mock.jwt.token, got %q", receivedAuth)
	}

	if !strings.Contains(receivedPath, "/users/usr_alice/orders?limit=5") {
		t.Errorf("unexpected path: %q", receivedPath)
	}

	if !strings.Contains(res.Content, "ord_1") {
		t.Errorf("expected ord_1 in response: %s", res.Content)
	}
}

func TestHTTPExecutor_POST_WithServiceToken(t *testing.T) {
	var (
		receivedAuth string
		receivedBody map[string]any
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true,"item_id":"item_99"}`))
	}))
	defer ts.Close()

	client := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: true}, 5*time.Second)
	auth := NewDownstreamAuthenticator(nil)

	def := core.ToolDefinition{
		ID:   "create_item",
		Name: "create_item",
		Executor: core.ExecutorConfig{
			Type:   "http",
			Method: "POST",
			URL:    ts.URL + "/items",
		},
		DownstreamAuth: core.DownstreamAuthConfig{
			Mode:      "service_token",
			SecretRef: "secret_service_key_123",
		},
	}

	exec := NewHTTPExecutor(def, client, auth)

	call := core.ToolCall{
		ID:        "call_2",
		Name:      "create_item",
		Arguments: `{"title":"Laptop","price":999}`,
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{Subject: "usr_bob"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected success: %s", res.Content)
	}

	if receivedAuth != "Bearer secret_service_key_123" {
		t.Errorf("expected Bearer secret_service_key_123, got %q", receivedAuth)
	}

	if receivedBody["title"] != "Laptop" || receivedBody["price"] != float64(999) {
		t.Errorf("unexpected body payload: %v", receivedBody)
	}
}

func TestHTTPExecutor_SignedHeadersAuth(t *testing.T) {
	var (
		sig     string
		tsVal   string
		subject string
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig = r.Header.Get("X-Beavend-Signature")
		tsVal = r.Header.Get("X-Beavend-Timestamp")
		subject = r.Header.Get("X-User-Subject")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"verified":true}`))
	}))
	defer ts.Close()

	client := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: true}, 5*time.Second)
	auth := NewDownstreamAuthenticator(nil)

	def := core.ToolDefinition{
		ID: "signed_op",
		Executor: core.ExecutorConfig{
			Type:   "http",
			Method: "POST",
			URL:    ts.URL + "/webhook",
		},
		DownstreamAuth: core.DownstreamAuthConfig{
			Mode:      "signed_headers",
			SecretRef: "hmac_master_secret",
		},
	}

	exec := NewHTTPExecutor(def, client, auth)

	call := core.ToolCall{
		ID:        "call_sig",
		Name:      "signed_op",
		Arguments: `{"action":"sync"}`,
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{Subject: "usr_carol"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected success: %s", res.Content)
	}

	if sig == "" || tsVal == "" || subject != "usr_carol" {
		t.Errorf("missing or invalid signed headers: sig=%q, ts=%q, subject=%q", sig, tsVal, subject)
	}
}

func TestHTTPExecutor_RetriesOn500(t *testing.T) {
	var attempts int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`internal server error`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"recovered"}`))
	}))
	defer ts.Close()

	client := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: true}, 5*time.Second)

	def := core.ToolDefinition{
		ID: "flaky_tool",
		Executor: core.ExecutorConfig{
			Type:    "http",
			Method:  "GET",
			URL:     ts.URL + "/flaky",
			Retries: 3,
		},
	}

	exec := NewHTTPExecutor(def, client, nil)

	call := core.ToolCall{
		ID:   "call_retry",
		Name: "flaky_tool",
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsError {
		t.Fatalf("expected success after retry: %s", res.Content)
	}

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestHTTPExecutor_ClientError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"resource not found"}`))
	}))
	defer ts.Close()

	client := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: true}, 5*time.Second)

	def := core.ToolDefinition{
		ID: "not_found_tool",
		Executor: core.ExecutorConfig{
			Type:   "http",
			Method: "GET",
			URL:    ts.URL + "/notfound",
		},
	}

	exec := NewHTTPExecutor(def, client, nil)

	call := core.ToolCall{
		ID:   "call_nf",
		Name: "not_found_tool",
	}

	res, err := exec.Execute(context.Background(), call, core.Identity{})
	if err != nil {
		t.Fatalf("unexpected error return on HTTP 404: %v", err)
	}

	if !res.IsError {
		t.Errorf("expected IsError: true for 404 response")
	}

	if !strings.Contains(res.Content, "resource not found") {
		t.Errorf("expected error message in content: %s", res.Content)
	}
}
