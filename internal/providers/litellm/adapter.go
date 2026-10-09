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

func (a *Adapter) imagesURL() string {
	if strings.HasSuffix(a.baseURL, "/v1") {
		return a.baseURL + "/images/generations"
	}
	return a.baseURL + "/v1/images/generations"
}

func (a *Adapter) speechURL() string {
	if strings.HasSuffix(a.baseURL, "/v1") {
		return a.baseURL + "/audio/speech"
	}
	return a.baseURL + "/v1/audio/speech"
}

func (a *Adapter) videoURL() string {
	if strings.HasSuffix(a.baseURL, "/v1") {
		return a.baseURL + "/video/generations"
	}
	return a.baseURL + "/v1/video/generations"
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

	const maxChatResponseBytes = 32 << 20 // 32MB limit
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxChatResponseBytes))
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

	//nolint:bodyclose // Body is closed asynchronously in the stream parser goroutine.
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
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
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

	const maxEmbedResponseBytes = 64 << 20 // 64MB limit
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxEmbedResponseBytes))
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

// Generate implements core.Generator for media generation via LiteLLM.
func (a *Adapter) Generate(ctx context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
	modality := strings.ToLower(strings.TrimSpace(req.Modality))
	switch modality {
	case "image":
		return a.generateImage(ctx, req)
	case "audio", "speech":
		return a.generateSpeech(ctx, req)
	case "video":
		return a.generateVideo(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported generation modality: %q", req.Modality)
	}
}

func (a *Adapter) generateImage(ctx context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
	count := req.Count
	if count <= 0 {
		count = 1
	}
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	quality := req.Quality
	if quality == "" {
		quality = "standard"
	}

	wireReq := providers.OpenAIImageRequest{
		Model:          req.Model,
		Prompt:         req.Prompt,
		N:              count,
		Size:           size,
		Quality:        quality,
		ResponseFormat: "url",
	}

	bodyBytes, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal image request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.imagesURL(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create image request: %w", err)
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
			Message: fmt.Sprintf("failed to reach litellm image endpoint: %v", err),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read image response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providers.NormalizeHTTPError(resp.StatusCode, respBody, resp.Header, "")
	}

	var wireResp providers.OpenAIImageResponse
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return nil, &core.ErrorResponse{
			Code:    "provider_error",
			Message: fmt.Sprintf("malformed image json response: %v", err),
		}
	}

	items := make([]core.GeneratedItem, len(wireResp.Data))
	for i, d := range wireResp.Data {
		mimeType := "image/png"
		if req.Format == "jpeg" || req.Format == "jpg" {
			mimeType = "image/jpeg"
		} else if req.Format == "webp" {
			mimeType = "image/webp"
		}
		items[i] = core.GeneratedItem{
			Index:         i,
			URL:           d.URL,
			MimeType:      mimeType,
			RevisedPrompt: d.RevisedPrompt,
		}
	}

	return &core.GenerateResponse{
		ID:        fmt.Sprintf("img_%d", time.Now().UnixNano()),
		Model:     req.Model,
		Modality:  "image",
		Items:     items,
		Usage:     core.UsageStats{Images: len(items)},
		CreatedAt: wireResp.Created,
	}, nil
}

func (a *Adapter) generateSpeech(ctx context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
	voice := req.Voice
	if voice == "" {
		voice = "alloy"
	}
	format := req.Format
	if format == "" {
		format = "mp3"
	}

	wireReq := providers.OpenAISpeechRequest{
		Model:          req.Model,
		Input:          req.Prompt,
		Voice:          voice,
		ResponseFormat: format,
	}

	bodyBytes, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal speech request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.speechURL(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create speech request: %w", err)
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
			Message: fmt.Sprintf("failed to reach litellm audio endpoint: %v", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, providers.NormalizeHTTPError(resp.StatusCode, respBody, resp.Header, "")
	}

	audioBytes, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return nil, fmt.Errorf("read audio response: %w", err)
	}

	mimeType := "audio/mpeg"
	switch format {
	case "opus":
		mimeType = "audio/opus"
	case "aac":
		mimeType = "audio/aac"
	case "flac":
		mimeType = "audio/flac"
	case "wav":
		mimeType = "audio/wav"
	}

	estDuration := req.Duration
	if estDuration <= 0 {
		estDuration = float64(len(req.Prompt)) / 15.0
		if estDuration < 1 {
			estDuration = 1
		}
	}

	item := core.GeneratedItem{
		Index:    0,
		Data:     audioBytes,
		MimeType: mimeType,
	}

	return &core.GenerateResponse{
		ID:        fmt.Sprintf("aud_%d", time.Now().UnixNano()),
		Model:     req.Model,
		Modality:  "audio",
		Items:     []core.GeneratedItem{item},
		Usage:     core.UsageStats{AudioSeconds: estDuration},
		CreatedAt: time.Now().Unix(),
	}, nil
}

func (a *Adapter) generateVideo(ctx context.Context, req core.GenerateRequest) (*core.GenerateResponse, error) {
	payload := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
	}
	if req.Duration > 0 {
		payload["duration"] = req.Duration
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal video request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.videoURL(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create video request: %w", err)
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
			Message: fmt.Sprintf("failed to reach litellm video endpoint: %v", err),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read video response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, providers.NormalizeHTTPError(resp.StatusCode, respBody, resp.Header, "")
	}

	var raw struct {
		ID   string `json:"id"`
		URL  string `json:"url"`
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	_ = json.Unmarshal(respBody, &raw)

	videoURL := raw.URL
	if videoURL == "" && len(raw.Data) > 0 {
		videoURL = raw.Data[0].URL
	}

	estDuration := req.Duration
	if estDuration <= 0 {
		estDuration = 5.0
	}

	item := core.GeneratedItem{
		Index:    0,
		URL:      videoURL,
		MimeType: "video/mp4",
	}

	resID := raw.ID
	if resID == "" {
		resID = fmt.Sprintf("vid_%d", time.Now().UnixNano())
	}

	return &core.GenerateResponse{
		ID:        resID,
		Model:     req.Model,
		Modality:  "video",
		Items:     []core.GeneratedItem{item},
		Usage:     core.UsageStats{VideoSeconds: estDuration},
		CreatedAt: time.Now().Unix(),
	}, nil
}

var (
	_ core.ProviderAdapter = (*Adapter)(nil)
	_ core.Generator       = (*Adapter)(nil)
)
