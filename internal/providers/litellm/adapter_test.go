package litellm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/providers"
)

func TestLiteLLMChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-master-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var req providers.OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-litellm-response-cost", "0.0025")
		finishReason := "stop"
		resp := providers.OpenAIChatResponse{
			ID:      "chatcmpl-test-1",
			Created: time.Now().Unix(),
			Choices: []providers.OpenAIChoice{
				{
					Index: 0,
					Message: providers.OpenAIMessage{
						Role:    "assistant",
						Content: "LiteLLM response content",
					},
					FinishReason: &finishReason,
				},
			},
			Usage: &providers.OpenAIUsage{
				PromptTokens:     12,
				CompletionTokens: 18,
				TotalTokens:      30,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter, err := NewAdapter(Config{
		BaseURL:   server.URL,
		MasterKey: "test-master-key",
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	chatReq := core.ChatRequest{
		Model: "gpt-4o",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Metadata: map[string]string{
			"request_id": "req_litellm_1",
			"ai_app_id":  "app_litellm_test",
		},
	}

	res, err := adapter.Chat(context.Background(), chatReq)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if res.ID != "chatcmpl-test-1" {
		t.Fatalf("expected ID chatcmpl-test-1, got %s", res.ID)
	}
	if res.Message.Content != "LiteLLM response content" {
		t.Fatalf("unexpected content: %s", res.Message.Content)
	}
	if res.Usage.TotalTokens != 30 {
		t.Fatalf("expected 30 tokens, got %d", res.Usage.TotalTokens)
	}
	if res.Usage.CostUSD == nil || *res.Usage.CostUSD != 0.0025 {
		t.Fatalf("expected cost 0.0025, got %v", res.Usage.CostUSD)
	}
}

func TestLiteLLMStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		chunks := []string{
			`data: {"id":"c-1","choices":[{"delta":{"content":"Hello"}}]}` + "\n\n",
			`data: {"id":"c-1","choices":[{"delta":{"content":" streaming"}}]}` + "\n\n",
			`data: {"id":"c-1","choices":[{"delta":{"content":" world!"}}]}` + "\n\n",
			`data: {"id":"c-1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer server.Close()

	adapter, err := NewAdapter(Config{
		BaseURL:   server.URL,
		MasterKey: "test-key",
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	streamChan, err := adapter.Stream(context.Background(), core.ChatRequest{
		Model: "gemini-2.5-flash",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "stream test"},
		},
		Metadata: map[string]string{
			"request_id": "req_stream_test",
		},
	})
	if err != nil {
		t.Fatalf("stream call: %v", err)
	}

	var text string
	var foundUsage bool
	for evt := range streamChan {
		switch evt.Type {
		case "delta":
			text += evt.Delta
		case "usage":
			foundUsage = true
			if evt.Usage.TotalTokens != 15 {
				t.Fatalf("expected 15 tokens, got %d", evt.Usage.TotalTokens)
			}
		}
	}

	if text != "Hello streaming world!" {
		t.Fatalf("expected 'Hello streaming world!', got %q", text)
	}
	if !foundUsage {
		t.Fatalf("expected usage event from stream")
	}
}

func TestLiteLLMEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := providers.OpenAIEmbedResponse{
			Object: "list",
			Model:  "text-embedding-ada-002",
			Data: []providers.OpenAIEmbeddingData{
				{
					Index:     0,
					Embedding: []float32{0.01, 0.02, 0.03},
				},
			},
			Usage: &providers.OpenAIUsage{
				PromptTokens: 5,
				TotalTokens:  5,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter, _ := NewAdapter(Config{BaseURL: server.URL})
	res, err := adapter.Embed(context.Background(), core.EmbedRequest{
		Model: "text-embedding-ada-002",
		Input: []string{"test embedding"},
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if len(res.Embeddings) != 1 || len(res.Embeddings[0]) != 3 {
		t.Fatalf("unexpected embeddings: %+v", res.Embeddings)
	}
}

func TestLiteLLMHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	adapter, _ := NewAdapter(Config{BaseURL: server.URL})
	if err := adapter.Health(context.Background()); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}

func TestLiteLLMErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded in LiteLLM","type":"rate_limit_error","code":429}}`))
	}))
	defer server.Close()

	adapter, _ := NewAdapter(Config{BaseURL: server.URL})
	_, err := adapter.Chat(context.Background(), core.ChatRequest{Model: "claude-3-7-sonnet"})
	if err == nil {
		t.Fatalf("expected error from 429 response")
	}

	var errResp *core.ErrorResponse
	if !errors.As(err, &errResp) {
		// It might be returned directly as *core.ErrorResponse
		t.Logf("err is %T: %v", err, err)
	}
	if errResp != nil {
		if errResp.Code != "rate_limited" {
			t.Fatalf("expected code rate_limited, got %s", errResp.Code)
		}
		if errResp.RetryAfter != 30 {
			t.Fatalf("expected retry_after 30, got %d", errResp.RetryAfter)
		}
	}
}
