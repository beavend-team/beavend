package stages

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/routing"
)

type mockProviderAdapter struct {
	chatFunc   func(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error)
	streamFunc func(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error)
}

func (m *mockProviderAdapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, req)
	}
	return &core.ChatResponse{}, nil
}

func (m *mockProviderAdapter) Stream(ctx context.Context, req core.ChatRequest) (<-chan core.StreamEvent, error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, req)
	}
	ch := make(chan core.StreamEvent)
	close(ch)
	return ch, nil
}

func (m *mockProviderAdapter) Embed(_ context.Context, _ core.EmbedRequest) (*core.EmbedResponse, error) {
	return nil, nil
}

func (m *mockProviderAdapter) Health(_ context.Context) error {
	return nil
}

func TestExecutionStage_HandleUnary(t *testing.T) {
	ctx := context.Background()
	resolver := routing.NewResolver()

	adapter := &mockProviderAdapter{
		chatFunc: func(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
			return &core.ChatResponse{
				Message: core.ChatMessage{
					Role:    "assistant",
					Content: "Order #123 has shipped.",
				},
				FinishReason: "stop",
				Usage: core.UsageStats{
					InputTokens:  15,
					OutputTokens: 5,
					TotalTokens:  20,
				},
			}, nil
		},
	}
	resolver.RegisterAdapter("litellm", adapter)

	stage, err := NewExecutionStage(resolver)
	require.NoError(t, err)

	state := &core.ReqState{
		RequestID:   "req_test_unary",
		AIAppID:     "assistant",
		IsStreaming: false,
		ResolvedModel: &core.ModelRouting{
			Adapter:      "litellm",
			LogicalModel: "fast",
			Deployment:   "gpt-4o-mini",
		},
		ChatRequest: &core.ChatRequest{
			Model: "gpt-4o-mini",
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Status?"},
			},
		},
	}

	err = stage.Handle(ctx, state)
	require.NoError(t, err)
	require.NotNil(t, state.ChatResponse)
	assert.Equal(t, "req_test_unary", state.ChatResponse.RequestID)
	assert.Equal(t, "assistant", state.ChatResponse.AIAppID)
	assert.Equal(t, "Order #123 has shipped.", state.ChatResponse.Message.Content)
	assert.Equal(t, 20, state.Usage.TotalTokens)
}

func TestExecutionStage_HandleUnaryError(t *testing.T) {
	ctx := context.Background()
	resolver := routing.NewResolver()

	adapter := &mockProviderAdapter{
		chatFunc: func(_ context.Context, _ core.ChatRequest) (*core.ChatResponse, error) {
			return nil, errors.New("upstream service unavailable")
		},
	}
	resolver.RegisterAdapter("litellm", adapter)

	stage, err := NewExecutionStage(resolver)
	require.NoError(t, err)

	state := &core.ReqState{
		RequestID:   "req_err",
		AIAppID:     "assistant",
		IsStreaming: false,
		ResolvedModel: &core.ModelRouting{
			Adapter: "litellm",
		},
		ChatRequest: &core.ChatRequest{
			Model: "gpt-4o",
		},
	}

	err = stage.Handle(ctx, state)
	require.Error(t, err)
	require.NotNil(t, state.Error)
	assert.Equal(t, "provider_error", state.Error.Code)
}

func TestExecutionStage_HandleStreaming(t *testing.T) {
	ctx := context.Background()
	resolver := routing.NewResolver()

	adapter := &mockProviderAdapter{
		streamFunc: func(_ context.Context, _ core.ChatRequest) (<-chan core.StreamEvent, error) {
			ch := make(chan core.StreamEvent, 5)
			go func() {
				ch <- core.StreamEvent{Type: "delta", Delta: "Hello "}
				ch <- core.StreamEvent{Type: "delta", Delta: "World!"}
				ch <- core.StreamEvent{
					Type: "usage",
					Usage: &core.UsageStats{
						InputTokens:  10,
						OutputTokens: 2,
						TotalTokens:  12,
					},
				}
				ch <- core.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	resolver.RegisterAdapter("litellm", adapter)

	stage, err := NewExecutionStage(resolver)
	require.NoError(t, err)

	state := &core.ReqState{
		RequestID:   "req_stream",
		AIAppID:     "assistant",
		IsStreaming: true,
		ResolvedModel: &core.ModelRouting{
			Adapter: "litellm",
		},
		ChatRequest: &core.ChatRequest{
			Model: "gpt-4o",
		},
	}

	err = stage.Handle(ctx, state)
	require.NoError(t, err)
	require.NotNil(t, state.StreamChan)

	var received []core.StreamEvent
	for evt := range state.StreamChan {
		received = append(received, evt)
	}

	require.Len(t, received, 4)
	assert.Equal(t, "delta", received[0].Type)
	assert.Equal(t, "Hello ", received[0].Delta)
	assert.Equal(t, "req_stream", received[0].RequestID)
	assert.Equal(t, "delta", received[1].Type)
	assert.Equal(t, "World!", received[1].Delta)
	assert.Equal(t, "usage", received[2].Type)
	assert.Equal(t, "done", received[3].Type)
}
