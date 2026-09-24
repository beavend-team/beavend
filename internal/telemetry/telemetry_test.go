package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogging_RedactionAndContext(t *testing.T) {
	var buf bytes.Buffer
	logger := SetupLogger("debug", &buf)

	ctx := context.Background()
	ctx = WithRequestID(ctx, "req_12345")
	ctx = WithProjectID(ctx, "proj_abc")
	ctx = WithEnvironment(ctx, "staging")

	logger.InfoContext(ctx, "user login attempt",
		slog.String("user", "alice"),
		slog.String("password", "supersecret123"),
		slog.String("bearer_token", "eyJhbGciOi..."),
		slog.String("api_key", "sk-123456"),
		slog.String("authorization", "Bearer topsecret"),
		slog.String("client_secret", "sec_999"),
	)

	output := buf.String()

	// Verify it's valid JSON
	var logMap map[string]any
	if err := json.Unmarshal([]byte(output), &logMap); err != nil {
		t.Fatalf("expected valid JSON log output, got error: %v, raw: %s", err, output)
	}

	// Verify standard attributes
	if logMap["time"] == nil || logMap["time"] == "" {
		t.Errorf("expected timestamp in log output, got %v", logMap["time"])
	}
	if logMap["level"] != "INFO" {
		t.Errorf("expected level 'INFO', got %v", logMap["level"])
	}
	if logMap["source"] == nil {
		t.Errorf("expected caller source in log output, got nil")
	}
	if logMap["msg"] != "user login attempt" {
		t.Errorf("expected msg 'user login attempt', got %v", logMap["msg"])
	}
	if logMap["request_id"] != "req_12345" {
		t.Errorf("expected request_id 'req_12345', got %v", logMap["request_id"])
	}
	if logMap["project_id"] != "proj_abc" {
		t.Errorf("expected project_id 'proj_abc', got %v", logMap["project_id"])
	}
	if logMap["environment"] != "staging" {
		t.Errorf("expected environment 'staging', got %v", logMap["environment"])
	}

	// Verify redaction
	sensitiveFields := []string{"password", "bearer_token", "api_key", "authorization", "client_secret"}
	for _, field := range sensitiveFields {
		val, exists := logMap[field]
		if !exists {
			t.Errorf("expected field %q to exist in log output", field)
			continue
		}
		if val != "[REDACTED]" {
			t.Errorf("expected field %q to be '[REDACTED]', got %v", field, val)
		}
	}

	// Ensure secret values do not appear anywhere in raw output
	rawSecrets := []string{"supersecret123", "eyJhbGciOi...", "sk-123456", "Bearer topsecret", "sec_999"}
	for _, sec := range rawSecrets {
		if strings.Contains(output, sec) {
			t.Errorf("raw output leaked secret: %s", sec)
		}
	}
}

func TestTelemetry_Metrics(t *testing.T) {
	ctx := context.Background()
	tel, err := Init(ctx, "beavend-test", "0.1.0")
	if err != nil {
		t.Fatalf("failed to init telemetry: %v", err)
	}
	defer func() {
		_ = tel.Shutdown(ctx)
	}()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	handler := tel.MetricsHandler()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK from /metrics, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "# HELP") {
		t.Errorf("expected prometheus metrics in response, got: %s", body)
	}
}
