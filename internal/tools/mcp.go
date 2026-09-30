package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/telemetry"
)

var (
	// ErrMCPProtocol indicates an issue with the MCP JSON-RPC protocol.
	ErrMCPProtocol = errors.New("mcp protocol error")
)

type mcpRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  string        `json:"method"`
	Params  mcpCallParams `json:"params"`
}

type mcpCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Result  *mcpCallResult `json:"result,omitempty"`
	Error   *mcpRPCError   `json:"error,omitempty"`
}

type mcpCallResult struct {
	Content []mcpContentItem `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

type mcpContentItem struct {
	Type     string         `json:"type"` // "text", "image", "resource"
	Text     string         `json:"text,omitempty"`
	Data     string         `json:"data,omitempty"`
	MimeType string         `json:"mimeType,omitempty"`
	Resource map[string]any `json:"resource,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCPExecutor executes tools provided by Model Context Protocol (MCP) servers via HTTP / SSE.
type MCPExecutor struct {
	def           core.ToolDefinition
	client        *http.Client
	authenticator core.Authenticator
}

// NewMCPExecutor creates a new MCPExecutor for the given tool definition.
func NewMCPExecutor(def core.ToolDefinition, client *http.Client, authenticator core.Authenticator) *MCPExecutor {
	if client == nil {
		client = NewSafeHTTPClient(SafeDialerConfig{}, 15*time.Second)
	}
	if authenticator == nil {
		authenticator = NewDownstreamAuthenticator(nil)
	}

	return &MCPExecutor{
		def:           def,
		client:        client,
		authenticator: authenticator,
	}
}

// Execute performs the MCP JSON-RPC 2.0 tools/call request and extracts the result content.
func (e *MCPExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: err.Error(),
			IsError: true,
		}, err
	}

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

	timeout := time.Duration(e.def.Executor.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	toolName := e.def.Name
	if toolName == "" {
		toolName = call.Name
	}
	if toolName == "" {
		toolName = e.def.ID
	}

	rpcReq := mcpRPCRequest{
		JSONRPC: "2.0",
		ID:      call.ID,
		Method:  "tools/call",
		Params: mcpCallParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	bodyBytes, err := json.Marshal(rpcReq)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("marshal rpc request: %v", err),
			IsError: true,
		}, err
	}

	rawURL := e.def.Executor.URL
	if rawURL == "" {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: "mcp executor URL is empty",
			IsError: true,
		}, fmt.Errorf("%w: empty mcp url", ErrToolExecution)
	}

	retries := e.def.Executor.Retries
	if retries < 0 {
		retries = 0
	}

	var (
		respBytes  []byte
		respStatus int
		respHeader http.Header
		lastErr    error
	)

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

		httpReq, err := http.NewRequestWithContext(execCtx, http.MethodPost, rawURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("create request: %v", err),
				IsError: true,
			}, err
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json, text/event-stream")

		for hk, hv := range e.def.Executor.Headers {
			httpReq.Header.Set(hk, hv)
		}

		if reqID := telemetry.RequestIDFromContext(ctx); reqID != "" {
			httpReq.Header.Set("X-Request-Id", reqID)
		}

		if e.def.DownstreamAuth.Mode != "" {
			if err := e.authenticator.Authenticate(execCtx, httpReq, &id, e.def.DownstreamAuth); err != nil {
				return core.ToolResult{
					CallID:  call.ID,
					ToolID:  e.def.ID,
					Content: fmt.Sprintf("authenticate mcp downstream request: %v", err),
					IsError: true,
				}, err
			}
		}

		resp, err := e.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		respStatus = resp.StatusCode
		respHeader = resp.Header.Clone()

		const maxMCPResponseBytes = 5 << 20 // 5MB max
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, maxMCPResponseBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		respBytes = b

		if respStatus >= 500 {
			lastErr = fmt.Errorf("server error status %d: %s", respStatus, string(respBytes))
			continue
		}

		lastErr = nil
		break
	}

	if lastErr != nil && len(respBytes) == 0 {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("MCP request failed: %v", lastErr),
			IsError: true,
		}, lastErr
	}

	if respStatus >= 400 {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: string(respBytes),
			IsError: true,
			Metadata: map[string]any{
				"status_code": respStatus,
				"url":         rawURL,
			},
		}, nil
	}

	// Parse JSON-RPC response (handling both standard JSON and SSE event payloads)
	extractedContent, isError, parseErr := parseMCPPayload(respBytes, respHeader.Get("Content-Type"))
	if parseErr != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: string(respBytes),
			IsError: true,
			Metadata: map[string]any{
				"parse_error": parseErr.Error(),
			},
		}, parseErr
	}

	return core.ToolResult{
		CallID:  call.ID,
		ToolID:  e.def.ID,
		Content: extractedContent,
		IsError: isError,
		Metadata: map[string]any{
			"status_code": respStatus,
			"url":         rawURL,
			"protocol":    "mcp-jsonrpc-2.0",
		},
	}, nil
}

func parseMCPPayload(payload []byte, contentType string) (string, bool, error) {
	if strings.Contains(contentType, "text/event-stream") {
		return parseMCPSSE(payload)
	}

	var rpcResp mcpRPCResponse
	if err := json.Unmarshal(payload, &rpcResp); err != nil {
		// Fallback: If not valid JSON-RPC structure, return raw string
		return string(payload), false, nil
	}

	return formatMCPResponse(&rpcResp)
}

func parseMCPSSE(payload []byte) (string, bool, error) {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	var lastData string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" && data != "[DONE]" {
				lastData = data
			}
		}
	}

	if lastData == "" {
		return string(payload), false, nil
	}

	var rpcResp mcpRPCResponse
	if err := json.Unmarshal([]byte(lastData), &rpcResp); err != nil {
		return lastData, false, nil
	}

	return formatMCPResponse(&rpcResp)
}

func formatMCPResponse(rpcResp *mcpRPCResponse) (string, bool, error) {
	if rpcResp.Error != nil {
		errMsg := fmt.Sprintf("MCP RPC Error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
		if rpcResp.Error.Data != nil {
			if dataBytes, err := json.Marshal(rpcResp.Error.Data); err == nil {
				errMsg += fmt.Sprintf(" - %s", string(dataBytes))
			}
		}
		return errMsg, true, nil
	}

	if rpcResp.Result == nil {
		return "", false, nil
	}

	isError := rpcResp.Result.IsError
	var sb strings.Builder

	for i, item := range rpcResp.Result.Content {
		if i > 0 {
			sb.WriteString("\n")
		}
		switch item.Type {
		case "text":
			sb.WriteString(item.Text)
		case "image":
			sb.WriteString(fmt.Sprintf("[Image (%s): %s]", item.MimeType, item.Data))
		case "resource":
			if resBytes, err := json.Marshal(item.Resource); err == nil {
				sb.WriteString(string(resBytes))
			} else {
				sb.WriteString(fmt.Sprintf("%v", item.Resource))
			}
		default:
			if item.Text != "" {
				sb.WriteString(item.Text)
			} else if item.Data != "" {
				sb.WriteString(item.Data)
			}
		}
	}

	content := sb.String()
	if content == "" && !isError {
		// If content array was empty, serialize entire result
		if resBytes, err := json.Marshal(rpcResp.Result); err == nil {
			content = string(resBytes)
		}
	}

	return content, isError, nil
}
