package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/telemetry"
)

var (
	// ErrToolExecution indicates a fatal or unrecoverable error during tool execution.
	ErrToolExecution = errors.New("tool execution error")
)

// HTTPExecutor executes downstream HTTP / REST endpoints for tools.
type HTTPExecutor struct {
	def           core.ToolDefinition
	client        *http.Client
	authenticator core.Authenticator
}

// NewHTTPExecutor creates an HTTPExecutor for a specific ToolDefinition.
func NewHTTPExecutor(def core.ToolDefinition, client *http.Client, authenticator core.Authenticator) *HTTPExecutor {
	if client == nil {
		client = NewSafeHTTPClient(SafeDialerConfig{}, 15*time.Second)
	}
	if authenticator == nil {
		authenticator = NewDownstreamAuthenticator(nil)
	}

	return &HTTPExecutor{
		def:           def,
		client:        client,
		authenticator: authenticator,
	}
}

// Execute performs the HTTP tool call and returns the resulting content or error.
func (e *HTTPExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: err.Error(),
			IsError: true,
		}, err
	}

	// Parse arguments JSON
	var args map[string]any
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("invalid arguments JSON: %v", err),
				IsError: true,
			}, fmt.Errorf("%w: invalid arguments: %v", ErrToolExecution, err)
		}
	} else {
		args = make(map[string]any)
	}

	// Determine timeout
	timeout := time.Duration(e.def.Executor.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build URL with templating
	rawURL := e.def.Executor.URL
	usedKeys := make(map[string]struct{})

	// Always substitute verified identity placeholders first
	if strings.Contains(rawURL, "{user_id}") {
		rawURL = strings.ReplaceAll(rawURL, "{user_id}", url.PathEscape(id.Subject))
		usedKeys["user_id"] = struct{}{}
	}
	if strings.Contains(rawURL, "{subject}") {
		rawURL = strings.ReplaceAll(rawURL, "{subject}", url.PathEscape(id.Subject))
		usedKeys["subject"] = struct{}{}
	}

	for k, v := range args {
		placeholder := fmt.Sprintf("{%s}", k)
		if strings.Contains(rawURL, placeholder) {
			rawURL = strings.ReplaceAll(rawURL, placeholder, url.PathEscape(fmt.Sprintf("%v", v)))
			usedKeys[k] = struct{}{}
		}
	}

	method := strings.ToUpper(strings.TrimSpace(e.def.Executor.Method))
	if method == "" {
		method = http.MethodGet
	}

	// Query parameters for GET/DELETE for remaining unused keys
	if method == http.MethodGet || method == http.MethodDelete {
		parsedURL, err := url.Parse(rawURL)
		if err == nil {
			q := parsedURL.Query()
			for k, v := range args {
				if _, used := usedKeys[k]; !used {
					q.Set(k, fmt.Sprintf("%v", v))
				}
			}
			parsedURL.RawQuery = q.Encode()
			rawURL = parsedURL.String()
		}
	}

	// Build request body for POST/PUT/PATCH
	var bodyBytes []byte
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		var err error
		bodyBytes, err = json.Marshal(args)
		if err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("marshal request body: %v", err),
				IsError: true,
			}, err
		}
	}

	retries := e.def.Executor.Retries
	if retries < 0 {
		retries = 0
	}

	var (
		respBody   string
		respStatus int
		lastErr    error
	)

	// Execute with retries
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*50) * time.Millisecond
			select {
			case <-execCtx.Done():
				return core.ToolResult{
					CallID:  call.ID,
					ToolID:  e.def.ID,
					Content: execCtx.Err().Error(),
					IsError: true,
				}, execCtx.Err()
			case <-time.After(backoff):
			}
		}

		var bodyReader io.Reader
		if len(bodyBytes) > 0 {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		httpReq, err := http.NewRequestWithContext(execCtx, method, rawURL, bodyReader)
		if err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("create request: %v", err),
				IsError: true,
			}, err
		}

		if len(bodyBytes) > 0 {
			httpReq.Header.Set("Content-Type", "application/json")
		}

		// Apply static configured headers
		for hk, hv := range e.def.Executor.Headers {
			httpReq.Header.Set(hk, hv)
		}

		// Propagate Request ID
		if reqID := telemetry.RequestIDFromContext(ctx); reqID != "" {
			httpReq.Header.Set("X-Request-Id", reqID)
		}

		// Apply downstream authentication
		if e.def.DownstreamAuth.Mode != "" {
			if err := e.authenticator.Authenticate(execCtx, httpReq, &id, e.def.DownstreamAuth); err != nil {
				return core.ToolResult{
					CallID:  call.ID,
					ToolID:  e.def.ID,
					Content: fmt.Sprintf("authenticate downstream request: %v", err),
					IsError: true,
				}, err
			}
		}

		resp, err := e.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue // Retry on network error
		}

		respStatus = resp.StatusCode
		b, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		respBody = string(b)

		// 5xx errors can be retried; 4xx and 2xx/3xx do not retry
		if respStatus >= 500 {
			lastErr = fmt.Errorf("server error status %d: %s", respStatus, respBody)
			continue
		}

		// Successful or non-retryable response received
		lastErr = nil
		break
	}

	if lastErr != nil && respBody == "" {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("HTTP request failed: %v", lastErr),
			IsError: true,
		}, lastErr
	}

	isError := respStatus >= 400
	return core.ToolResult{
		CallID:  call.ID,
		ToolID:  e.def.ID,
		Content: respBody,
		IsError: isError,
		Metadata: map[string]any{
			"status_code": respStatus,
			"url":         rawURL,
			"method":      method,
		},
	}, nil
}
