package litellm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/providers"
)

// Config configures the LiteLLM adapter.
type Config struct {
	BaseURL    string
	MasterKey  string
	HTTPClient *http.Client
}

// Adapter implements core.ProviderAdapter for the LiteLLM proxy gateway.
type Adapter struct {
	baseURL         string
	masterKey       string
	httpClient      *http.Client
	streamingClient *http.Client
}

// NewAdapter creates a new LiteLLM Adapter.
func NewAdapter(cfg Config) (*Adapter, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 50,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}

	streamingClient := &http.Client{
		Timeout:   0, // Unlimited duration for persistent SSE streams governed by ctx
		Transport: client.Transport,
	}

	return &Adapter{
		baseURL:         baseURL,
		masterKey:       strings.TrimSpace(cfg.MasterKey),
		httpClient:      client,
		streamingClient: streamingClient,
	}, nil
}

func (a *Adapter) chatURL() string {
	if strings.HasSuffix(a.baseURL, "/v1") {
		return a.baseURL + "/chat/completions"
	}
	return a.baseURL + "/v1/chat/completions"
}

func (a *Adapter) embedURL() string {
	if strings.HasSuffix(a.baseURL, "/v1") {
		return a.baseURL + "/embeddings"
	}
	return a.baseURL + "/v1/embeddings"
}

func (a *Adapter) healthURL() string {
	return a.baseURL + "/health"
}

// Chat executes a synchronous, unary chat completion request against LiteLLM.
func (a *Adapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	wireReq := providers.FromCoreChatRequest(req, req.Model)
	wireReq.Stream = false
	wireReq.StreamOptions = nil

	bodyBytes, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.chatURL(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create chat http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if a.masterKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.masterKey)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &core.ErrorResponse{
			Code:    "unavailable",
			Message: fmt.Sprintf("failed to connect to litellm gateway: %v", err),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read litellm response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		reqID := req.Metadata["request_id"]
		return nil, providers.NormalizeHTTPError(resp.StatusCode, respBody, resp.Header, reqID)
	}

	var wireResp providers.OpenAIChatResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, &core.ErrorResponse{
			Code:    "provider_error",
			Message: fmt.Sprintf("malformed json from litellm: %v", err),
		}
	}

	// Parse custom cost header if provided by LiteLLM
	var headerCost *float64
	if costStr := resp.Header.Get("x-litellm-response-cost"); costStr != "" {
		if costVal, err := strconv.ParseFloat(costStr, 64); err == nil {
			headerCost = &costVal
		}
	}

	reqID := req.Metadata["request_id"]
	aiAppID := req.Metadata["ai_app_id"]
	return providers.ToCoreChatResponse(&wireResp, reqID, aiAppID, headerCost), nil
}

// Stream executes a streaming chat completion request, returning a channel of StreamEvents.
func (a *Adapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	wireReq := providers.FromCoreChatRequest(req, req.Model)
	wireReq.Stream = true

	bodyBytes, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.chatURL(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create stream http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if a.masterKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.masterKey)
	}

	resp, err := a.streamingClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &core.ErrorResponse{
			Code:    "unavailable",
			Message: fmt.Sprintf("failed to connect to litellm gateway: %v", err),
		}
	}

	reqID := req.Metadata["request_id"]
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, providers.NormalizeHTTPError(resp.StatusCode, respBody, resp.Header, reqID)
	}

	out := make(chan core.StreamEvent, 64)
	parser := providers.NewStreamParser(reqID)

	go func() {
		defer resp.Body.Close()
		parser.ParseStream(ctx, resp.Body, out)
	}()

	return out, nil
}

// Embed generates vector embeddings for the provided input strings.
func (a *Adapter) Embed(ctx context.Context, req core.EmbedRequest) (*core.EmbedResponse, error) {
	wireReq := providers.FromCoreEmbedRequest(req, req.Model)

	bodyBytes, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.embedURL(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create embed http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if a.masterKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.masterKey)
	}

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &core.ErrorResponse{
			Code:    "unavailable",
			Message: fmt.Sprintf("failed to connect to litellm gateway: %v", err),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embed response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providers.NormalizeHTTPError(resp.StatusCode, respBody, resp.Header, "")
	}

	var wireResp providers.OpenAIEmbedResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, &core.ErrorResponse{
			Code:    "provider_error",
			Message: fmt.Sprintf("malformed embed response from litellm: %v", err),
		}
	}

	return providers.ToCoreEmbedResponse(&wireResp), nil
}

// Health verifies the availability of the LiteLLM gateway.
func (a *Adapter) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.healthURL(), nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}

	if a.masterKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.masterKey)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("litellm health check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("litellm health check returned status %d", resp.StatusCode)
	}

	return nil
}
