package stages

import (
	"context"
	"strings"
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

func TestContextStage_LatestCompactionSummaryCheckpoint(t *testing.T) {
	ctx := context.Background()
	threadStore := memory.NewMemoryThreadStore()
	stage := NewContextStage(nil, threadStore)

	// Seed thread with older turns, a compaction checkpoint, and recent turns
	_ = threadStore.CreateThread(ctx, &core.Thread{
		ID:          "th_checkpoint_test",
		ProjectID:   "proj1",
		Environment: "prod",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Old turn 1"},
			{Role: "assistant", Content: "Old reply 1"},
			{
				Role:    "system",
				Content: "Summary of previous conversation:\nUser setup account.",
				Metadata: map[string]any{
					"type": memory.CompactionSummaryMetadataType,
				},
			},
			{Role: "user", Content: "Recent turn 2"},
			{Role: "assistant", Content: "Recent reply 2"},
		},
	})

	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		ChatRequest: &core.ChatRequest{
			ThreadID: "th_checkpoint_test",
			Messages: []core.ChatMessage{
				{Role: "user", Content: "New prompt 3"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)

	// Should omit older turns 1 & 2, starting directly from compaction summary checkpoint!
	require.Len(t, state.ChatRequest.Messages, 4)
	assert.Equal(t, "system", state.ChatRequest.Messages[0].Role)
	assert.Contains(t, state.ChatRequest.Messages[0].Content, "Summary of previous conversation:")
	assert.Equal(t, "Recent turn 2", state.ChatRequest.Messages[1].Content)
	assert.Equal(t, "Recent reply 2", state.ChatRequest.Messages[2].Content)
	assert.Equal(t, "New prompt 3", state.ChatRequest.Messages[3].Content)
}

func TestContextStage_ToolOutputTruncationOlderThanTwoTurns(t *testing.T) {
	ctx := context.Background()
	stage := NewContextStage(nil, nil)

	longOutput := strings.Repeat("Extremely verbose tool output. ", 20) // ~600 chars

	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		AIApp: &core.AIApp{
			Memory: core.MemoryConfig{
				MaxTokens: 50000, // Large enough to prevent turn-group dropping in Step 2
			},
		},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				// Turn 1 (>2 dialogue cycles old)
				{Role: "user", Content: "Turn 1: fetch data"},
				{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{ID: "call_1", Name: "fetch_data"},
					},
				},
				{Role: "tool", ToolCallID: "call_1", Content: longOutput},
				{Role: "assistant", Content: "Processed data 1."},

				// Turn 2 (within last 2 dialogue cycles: cycle 2)
				{Role: "user", Content: "Turn 2: query data"},
				{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{ID: "call_2", Name: "query_data"},
					},
				},
				{Role: "tool", ToolCallID: "call_2", Content: longOutput},
				{Role: "assistant", Content: "Processed data 2."},

				// Turn 3 (within last 2 dialogue cycles: cycle 1)
				{Role: "user", Content: "Turn 3: question"},
				{Role: "assistant", Content: "Answer 3."},

				// Turn 4 (current query)
				{Role: "user", Content: "Turn 4: latest question"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)

	var foundCall1, foundCall2 bool
	for _, m := range state.ChatRequest.Messages {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			foundCall1 = true
			assert.LessOrEqual(t, len(m.Content), 200+len("...[truncated]"))
			assert.Contains(t, m.Content, "...[truncated]")
		}
		if m.Role == "tool" && m.ToolCallID == "call_2" {
			foundCall2 = true
			assert.Equal(t, longOutput, m.Content, "tool output in recent 2 dialogue cycles must NOT be truncated")
		}
	}
	assert.True(t, foundCall1, "call_1 must be present")
	assert.True(t, foundCall2, "call_2 must be present")
}

func TestContextStage_PairPreservingTurnDrop(t *testing.T) {
	ctx := context.Background()
	stage := NewContextStage(nil, nil)

	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		AIApp: &core.AIApp{
			Memory: core.MemoryConfig{
				MaxHistory: 2, // Retain max 2 groups
			},
		},
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				{Role: "system", Content: "Base instructions"},
				// Group 1: user
				{Role: "user", Content: "Old user message"},
				// Group 2: assistant with tool calls + tool results
				{
					Role: "assistant",
					ToolCalls: []core.ToolCall{
						{ID: "call_x", Name: "tool_x"},
					},
				},
				{Role: "tool", ToolCallID: "call_x", Content: "result x"},
				// Group 3: user
				{Role: "user", Content: "Latest query"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)

	// Group 1 was dropped. Group 2 (assistant tool_call + tool result) was preserved intact!
	// Plus Group 3 (latest query).
	// Total messages: 1 system + 2 (assistant + tool) + 1 (user) = 4
	require.Len(t, state.ChatRequest.Messages, 4)
	assert.Equal(t, "system", state.ChatRequest.Messages[0].Role)
	assert.Equal(t, "assistant", state.ChatRequest.Messages[1].Role)
	require.Len(t, state.ChatRequest.Messages[1].ToolCalls, 1)
	assert.Equal(t, "call_x", state.ChatRequest.Messages[1].ToolCalls[0].ID)
	assert.Equal(t, "tool", state.ChatRequest.Messages[2].Role)
	assert.Equal(t, "call_x", state.ChatRequest.Messages[2].ToolCallID)
	assert.Equal(t, "Latest query", state.ChatRequest.Messages[3].Content)
}

func TestContextStage_SnapshotDocumentMemoryHydration(t *testing.T) {
	ctx := context.Background()
	stage := NewContextStage(nil, nil)

	snap := core.NewSnapshot("proj1", "prod", 1)
	snap.DocumentMemories["project::company_name"] = &core.DocumentMemory{
		Scope: core.DocumentMemoryScopeProject,
		Key:   "company_name",
		Value: "Beavend Technologies",
	}
	snap.DocumentMemories["app:app_support:tone"] = &core.DocumentMemory{
		Scope:    core.DocumentMemoryScopeApp,
		TargetID: "app_support",
		Key:      "tone",
		Value:    "friendly and technical",
	}
	snap.DocumentMemories["app:app_other:secret"] = &core.DocumentMemory{
		Scope:    core.DocumentMemoryScopeApp,
		TargetID: "app_other",
		Key:      "secret",
		Value:    "other app secret",
	}

	state := &core.ReqState{
		ProjectID:   "proj1",
		Environment: "prod",
		AIAppID:     "app_support",
		Snapshot:    snap,
		ChatRequest: &core.ChatRequest{
			Messages: []core.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		},
	}

	err := stage.Handle(ctx, state)
	require.NoError(t, err)

	// Document memory system message must be injected
	require.GreaterOrEqual(t, len(state.ChatRequest.Messages), 2)
	docMsg := state.ChatRequest.Messages[0]
	assert.Equal(t, "system", docMsg.Role)
	assert.Contains(t, docMsg.Content, "Project Guidelines & Document Memory:")
	assert.Contains(t, docMsg.Content, "Beavend Technologies")
	assert.Contains(t, docMsg.Content, "friendly and technical")
	assert.NotContains(t, docMsg.Content, "other app secret", "must not leak document memory belonging to other apps")
}

func TestContextStage_AttachmentModalityAdaptation(t *testing.T) {
	ctx := context.Background()
	stage := NewContextStage(nil, nil)

	snap := &core.Snapshot{
		Models: map[string]*core.ModelRouting{
			"vision-only-model": {
				LogicalModel:    "vision-only-model",
				InputModalities: []string{"text", "image"},
			},
			"doc-capable-model": {
				LogicalModel:    "doc-capable-model",
				InputModalities: []string{"text", "image", "document"},
			},
		},
	}

	t.Run("adapts unsupported PDF attachment when routed to vision-only model", func(t *testing.T) {
		state := &core.ReqState{
			ProjectID:     "proj1",
			Environment:   "prod",
			Snapshot:      snap,
			SelectedModel: "vision-only-model",
			ChatRequest: &core.ChatRequest{
				Messages: []core.ChatMessage{
					{Role: "user", Content: "Please review the attached document."},
				},
				Attachments: []core.Attachment{
					{
						Type:     "document",
						MimeType: "application/pdf",
						Data:     []byte("%PDF-1.3 fake pdf content"),
						Filename: "quarterly_report.pdf",
					},
				},
			},
		}

		err := stage.Handle(ctx, state)
		require.NoError(t, err)

		// Attachment must be removed from Attachments array so it is not sent as an unhandled file block
		assert.Empty(t, state.ChatRequest.Attachments)

		// Placeholder must be appended to user message
		assert.Contains(t, state.ChatRequest.Messages[0].Content, "[document: quarterly_report.pdf (input modality not supported by target model vision-only-model)]")
	})

	t.Run("retains PDF attachment when routed to document-capable model", func(t *testing.T) {
		state := &core.ReqState{
			ProjectID:     "proj1",
			Environment:   "prod",
			Snapshot:      snap,
			SelectedModel: "doc-capable-model",
			ChatRequest: &core.ChatRequest{
				Messages: []core.ChatMessage{
					{Role: "user", Content: "Please review the attached document."},
				},
				Attachments: []core.Attachment{
					{
						Type:     "document",
						MimeType: "application/pdf",
						Data:     []byte("%PDF-1.3 fake pdf content"),
						Filename: "quarterly_report.pdf",
					},
				},
			},
		}

		err := stage.Handle(ctx, state)
		require.NoError(t, err)

		// Attachment must be retained
		require.Len(t, state.ChatRequest.Attachments, 1)
		assert.Equal(t, "quarterly_report.pdf", state.ChatRequest.Attachments[0].Filename)
	})
}
