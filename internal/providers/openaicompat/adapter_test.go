package openaicompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/providers"
)

func TestOpenAICompatChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("OpenAI-Organization") != "org-123" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-Custom") != "val" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		finishReason := "stop"
		resp := providers.OpenAIChatResponse{
			ID:      "chatcmpl-compat-1",
			Created: time.Now().Unix(),
			Choices: []providers.OpenAIChoice{
				{
					Index: 0,
					Message: providers.OpenAIMessage{
						Role:    "assistant",
						Content: "Direct OpenAI response",
					},
					FinishReason: &finishReason,
				},
			},
			Usage: &providers.OpenAIUsage{
				PromptTokens:     10,
				CompletionTokens: 10,
				TotalTokens:      20,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter, err := NewAdapter(Config{
		BaseURL:      server.URL,
		APIKey:       "test-api-key",
		Organization: "org-123",
		ExtraHeaders: map[string]string{"X-Custom": "val"},
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	res, err := adapter.Chat(context.Background(), core.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "hi"},
		},
		Metadata: map[string]string{
			"request_id": "req_compat_1",
		},
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if res.Message.Content != "Direct OpenAI response" {
		t.Fatalf("unexpected content: %s", res.Message.Content)
	}
	if res.Usage.TotalTokens != 20 {
		t.Fatalf("expected 20 tokens, got %d", res.Usage.TotalTokens)
	}
}

func TestOpenAICompatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"token1"}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":" token2"}}]}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: [DONE]` + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	adapter, _ := NewAdapter(Config{BaseURL: server.URL})
	streamChan, err := adapter.Stream(context.Background(), core.ChatRequest{
		Model: "deepseek-r1",
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var output string
	for evt := range streamChan {
		if evt.Type == "delta" {
			output += evt.Delta
		}
	}

	if output != "token1 token2" {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestOpenAICompatEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := providers.OpenAIEmbedResponse{
			Object: "list",
			Model:  "text-embedding-3-small",
			Data: []providers.OpenAIEmbeddingData{
				{Index: 0, Embedding: []float32{0.5, 0.5}},
			},
			Usage: &providers.OpenAIUsage{
				PromptTokens: 2,
				TotalTokens:  2,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter, _ := NewAdapter(Config{BaseURL: server.URL})
	res, err := adapter.Embed(context.Background(), core.EmbedRequest{
		Model: "text-embedding-3-small",
		Input: []string{"test"},
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if len(res.Embeddings) != 1 || res.Embeddings[0][0] != 0.5 {
		t.Fatalf("unexpected embeddings: %v", res.Embeddings)
	}
}

func TestOpenAICompatHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	adapter, _ := NewAdapter(Config{BaseURL: server.URL})
	if err := adapter.Health(context.Background()); err != nil {
		t.Fatalf("health failed: %v", err)
	}
}
