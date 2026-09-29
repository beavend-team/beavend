package stages

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/memory"
)

func TestContextStage_ThreadHistoryPrepending(t *testing.T) {
	ctx := context.Background()
	threadStore := memory.NewMemoryThreadStore()
	stage := NewContextStage(nil, threadStore)

	// Seed existing thread with 2 turns
	_ = threadStore.CreateThread(ctx, &core.Thread{
		ID:          "th_test_1",
		ProjectID:   "proj1",
		Environment: "prod",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	})

	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		ChatRequest: &core.ChatRequest{
			ThreadID: "th_test_1",
			Messages: []core.ChatMessage{
				{Role: "user", Content: "What can you do?"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)

	// State should now contain 3 messages: 2 from thread + 1 new turn
	require.Len(t, state.ChatRequest.Messages, 3)
	assert.Equal(t, "Hello", state.ChatRequest.Messages[0].Content)
	assert.Equal(t, "Hi there!", state.ChatRequest.Messages[1].Content)
	assert.Equal(t, "What can you do?", state.ChatRequest.Messages[2].Content)
}

func TestContextStage_SubjectIsolation(t *testing.T) {
	ctx := context.Background()
	threadStore := memory.NewMemoryThreadStore()
	stage := NewContextStage(nil, threadStore)

	// Seed thread owned by alice
	_ = threadStore.CreateThread(ctx, &core.Thread{
		ID:          "th_alice",
		ProjectID:   "proj1",
		Environment: "prod",
		Issuer:      "iss1",
		Subject:     "alice",
	})

	// Request from bob
	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		Identity: &core.Identity{
			Issuer:  "iss1",
			Subject: "bob",
		},
		ChatRequest: &core.ChatRequest{
			ThreadID: "th_alice",
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Sneaking in"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.Error(t, err)
	assert.Equal(t, "forbidden", state.Error.Code)
}

func TestContextStage_MemoryRecall(t *testing.T) {
	ctx := context.Background()
	localMem := memory.NewLocalMemoryProvider()
	engine := memory.NewEngine(memory.EngineConfig{
		DefaultProvider: localMem,
	})
	stage := NewContextStage(engine, nil)

	// Seed memory
	_ = localMem.Store(ctx, core.MemoryRecord{
		ProjectID:   "proj1",
		Environment: "prod",
		Issuer:      "iss1",
		Subject:     "alice",
		AIAppID:     "app1",
		Content:     "Alice loves espresso coffee",
	})

	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app1",
		AIApp: &core.AIApp{
			ID: "app1",
			Memory: core.MemoryConfig{
				Enabled:      true,
				VectorRecall: true,
				RecallLimit:  2,
			},
		},
		Identity: &core.Identity{
			Issuer:  "iss1",
			Subject: "alice",
		},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Make me a drink recommendation"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)

	// Memory injected as first system message
	require.Len(t, state.ChatRequest.Messages, 2)
	assert.Equal(t, "system", state.ChatRequest.Messages[0].Role)
	assert.Contains(t, state.ChatRequest.Messages[0].Content, "Alice loves espresso coffee")
}
