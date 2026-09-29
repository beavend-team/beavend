package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/providers"
)

// Config holds configuration for OpenAICompatAdapter.
type Config struct {
	BaseURL      string
	APIKey       string
	Organization string
	ExtraHeaders map[string]string
	HTTPClient   *http.Client
}

// Adapter implements core.ProviderAdapter directly calling OpenAI-compatible APIs.
type Adapter struct {
	baseURL         string
	apiKey          string
	organization    string
	extraHeaders    map[string]string
	httpClient      *http.Client
	streamingClient *http.Client
}

// NewAdapter creates a new native OpenAICompatAdapter.
func NewAdapter(cfg Config) (*Adapter, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 100,
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
		apiKey:          strings.TrimSpace(cfg.APIKey),
		organization:    strings.TrimSpace(cfg.Organization),
		extraHeaders:    cfg.ExtraHeaders,
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

func (a *Adapter) modelsURL() string {
	if strings.HasSuffix(a.baseURL, "/v1") {
		return a.baseURL + "/models"
	}
	return a.baseURL + "/v1/models"
}

func (a *Adapter) applyHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	if a.organization != "" {
		req.Header.Set("OpenAI-Organization", a.organization)
	}
	for k, v := range a.extraHeaders {
		req.Header.Set(k, v)
	}
}

// Chat executes a synchronous unary chat completion.
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
	a.applyHeaders(httpReq)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &core.ErrorResponse{
			Code:    "unavailable",
			Message: fmt.Sprintf("failed to reach provider endpoint: %v", err),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	reqID := req.Metadata["request_id"]
	if resp.StatusCode != http.StatusOK {
		return nil, providers.NormalizeHTTPError(resp.StatusCode, respBody, resp.Header, reqID)
	}

	var wireResp providers.OpenAIChatResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, &core.ErrorResponse{
			Code:    "provider_error",
			Message: fmt.Sprintf("malformed json from provider: %v", err),
		}
	}

	aiAppID := req.Metadata["ai_app_id"]
	return providers.ToCoreChatResponse(&wireResp, reqID, aiAppID, nil), nil
}

// Stream executes a streaming chat completion, yielding a channel of StreamEvents.
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
	a.applyHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	//nolint:bodyclose // Body is closed asynchronously in the stream parser goroutine.
	resp, err := a.streamingClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &core.ErrorResponse{
			Code:    "unavailable",
			Message: fmt.Sprintf("failed to reach provider endpoint: %v", err),
		}
	}

	reqID := req.Metadata["request_id"]
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
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

// Embed generates vector embeddings for input strings.
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
	a.applyHeaders(httpReq)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &core.ErrorResponse{
			Code:    "unavailable",
			Message: fmt.Sprintf("failed to reach provider endpoint: %v", err),
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
			Message: fmt.Sprintf("malformed embed json from provider: %v", err),
		}
	}

	return providers.ToCoreEmbedResponse(&wireResp), nil
}

// Health checks the endpoint by performing a GET on /models.
func (a *Adapter) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.modelsURL(), nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}
	a.applyHeaders(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("endpoint health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("endpoint health check failed with status %d", resp.StatusCode)
	}

	return nil
}
