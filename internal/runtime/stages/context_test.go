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

func TestContextStage_MidThreadModelSwitching(t *testing.T) {
	ctx := context.Background()
	threadStore := memory.NewMemoryThreadStore()
	stage := NewContextStage(nil, threadStore)

	snap := &core.Snapshot{
		Models: map[string]*core.ModelRouting{
			"vision-model": {
				LogicalModel:    "vision-model",
				InputModalities: []string{"text", "image"},
			},
			"text-only-model": {
				LogicalModel:    "text-only-model",
				InputModalities: []string{"text"},
			},
		},
	}

	threadID := "th_switching_123"

	// --- Turn 1: User uploads image with vision model ---
	turn1Msg := core.ChatMessage{
		Role: "user",
		Parts: []core.Part{
			core.TextPart("What is in this photo?"),
			core.MediaPart(&core.MediaRef{
				MediaID:  "med_dog",
				MimeType: "image/jpeg",
				Digest:   "sha256:dog123",
				Caption:  "A golden retriever puppy",
			}),
		},
	}
	turn1Assistant := core.ChatMessage{
		Role:    "assistant",
		Content: "It is a golden retriever puppy playing with a ball.",
	}

	// Persist Turn 1 into thread store (using canonical parts, never provider-native)
	_ = threadStore.CreateThread(ctx, &core.Thread{
		ID:          threadID,
		ProjectID:   "proj1",
		Environment: "prod",
		Messages:    []core.ChatMessage{turn1Msg, turn1Assistant},
	})

	// --- Turn 2: Routed to text-only model ---
	stateTurn2 := &core.ReqState{
		ProjectID:     "proj1",
		Environment:   "prod",
		Snapshot:      snap,
		SelectedModel: "text-only-model",
		ChatRequest: &core.ChatRequest{
			ThreadID: threadID,
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Can you summarize its behavior in bullet points?"},
			},
		},
	}

	err := stage.Handle(ctx, stateTurn2)
	require.NoError(t, err)

	// In stateTurn2, the image turn from thread history must have been substituted with a placeholder
	require.Len(t, stateTurn2.ChatRequest.Messages, 3)
	historyTurn1 := stateTurn2.ChatRequest.Messages[0]
	assert.Equal(t, "user", historyTurn1.Role)
	require.Len(t, historyTurn1.Parts, 2)
	assert.Equal(t, core.PartTypeText, historyTurn1.Parts[0].Type)
	assert.Equal(t, core.PartTypeText, historyTurn1.Parts[1].Type)
	assert.Contains(t, historyTurn1.Parts[1].Text, "[image: sha256:dog123 (A golden retriever puppy)]")
	assert.Contains(t, historyTurn1.Content, "[image: sha256:dog123 (A golden retriever puppy)]")

	// Append Turn 2 response to the thread
	turn2Assistant := core.ChatMessage{
		Role:    "assistant",
		Content: "- Active\n- Energetic",
	}
	_ = threadStore.AppendMessages(ctx, "proj1", "prod", threadID, []core.ChatMessage{
		stateTurn2.ChatRequest.Messages[2],
		turn2Assistant,
	}, 0)

	// Verify original thread still maintains canonical parts
	storedThread, err := threadStore.GetThread(ctx, "proj1", "prod", threadID)
	require.NoError(t, err)
	require.Len(t, storedThread.Messages, 4)
	assert.Equal(t, core.PartTypeMedia, storedThread.Messages[0].Parts[1].Type)
	assert.Equal(t, "med_dog", storedThread.Messages[0].Parts[1].Media.MediaID)

	// --- Turn 3: Routed to vision model ---
	stateTurn3 := &core.ReqState{
		ProjectID:     "proj1",
		Environment:   "prod",
		Snapshot:      snap,
		SelectedModel: "vision-model",
		ChatRequest: &core.ChatRequest{
			ThreadID: threadID,
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Is there any collar visible in the first picture?"},
			},
		},
	}

	err = stage.Handle(ctx, stateTurn3)
	require.NoError(t, err)

	// Because vision-model supports image modality, Turn 1 media part is preserved!
	require.Len(t, stateTurn3.ChatRequest.Messages, 5)
	turn1Revisited := stateTurn3.ChatRequest.Messages[0]
	require.Len(t, turn1Revisited.Parts, 2)
	assert.Equal(t, core.PartTypeMedia, turn1Revisited.Parts[1].Type)
	assert.Equal(t, "med_dog", turn1Revisited.Parts[1].Media.MediaID)
}

func TestContextStage_MaxHistoryPruning(t *testing.T) {
	ctx := context.Background()
	stage := NewContextStage(nil, nil)

	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		AIApp: &core.AIApp{
			Memory: core.MemoryConfig{
				MaxHistory: 2, // only keep 2 non-system turns
			},
		},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				{Role: "system", Content: "System prompt"},
				{Role: "user", Content: "Turn 1"},
				{Role: "assistant", Content: "Reply 1"},
				{Role: "user", Content: "Turn 2"},
				{Role: "assistant", Content: "Reply 2"},
				{Role: "user", Content: "Turn 3"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)

	// Should preserve system prompt + the last 2 turns (Reply 2 and Turn 3)
	require.Len(t, state.ChatRequest.Messages, 3)
	assert.Equal(t, "system", state.ChatRequest.Messages[0].Role)
	assert.Equal(t, "Reply 2", state.ChatRequest.Messages[1].Content)
	assert.Equal(t, "Turn 3", state.ChatRequest.Messages[2].Content)
}
