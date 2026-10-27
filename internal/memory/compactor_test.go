package memory

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

type mockCompactorAdapter struct {
	chatFunc func(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error)
}

func (m *mockCompactorAdapter) Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, req)
	}
	return &core.ChatResponse{
		ID: "resp_summary",
		Message: core.ChatMessage{
			Role:    "assistant",
			Content: "User previously asked for account cancellation. System verified order #10 was processed.",
		},
		Usage: core.UsageStats{TotalTokens: 45},
	}, nil
}

type mockCompactorUsageStore struct {
	mu       sync.Mutex
	recorded []*core.ReqState
}

func (m *mockCompactorUsageStore) RecordUsage(_ context.Context, state *core.ReqState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recorded = append(m.recorded, state)
	return nil
}

func (m *mockCompactorUsageStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.recorded)
}

func TestCompactor_AsynchronousSummarization(t *testing.T) {
	ctx := context.Background()
	threadStore := NewMemoryThreadStore()
	usageStore := &mockCompactorUsageStore{}
	adapter := &mockCompactorAdapter{}

	compactor := NewCompactor(CompactorConfig{
		ThreadStore: threadStore,
		UsageStore:  usageStore,
		Adapter:     adapter,
		Model:       "fast",
		Threshold:   0.75,
	})

	// Setup a long thread with 6 messages
	thread := &core.Thread{
		ID:          "th_compaction_test",
		ProjectID:   "proj_test",
		Environment: "dev",
		AIAppID:     "app_test",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello, I want to cancel my order."},
			{Role: "assistant", Content: "Which order number?"},
			{Role: "user", Content: "Order #10."},
			{Role: "assistant", Content: "Processing order #10 cancellation."},
			{Role: "user", Content: "Now please confirm my refund."},
			{Role: "assistant", Content: "Checking refund status..."},
		},
	}
	require.NoError(t, threadStore.CreateThread(ctx, thread))

	// Compact thread
	err := compactor.CompactThread(ctx, "proj_test", "dev", "app_test", "th_compaction_test")
	require.NoError(t, err)

	// Verify updated thread in ThreadStore
	updatedTh, err := threadStore.GetThread(ctx, "proj_test", "dev", "th_compaction_test")
	require.NoError(t, err)
	require.NotEmpty(t, updatedTh.Messages)

	// The first message MUST be a system compaction summary checkpoint
	firstMsg := updatedTh.Messages[0]
	assert.Equal(t, "system", firstMsg.Role)
	assert.Contains(t, firstMsg.Content, "Summary of previous conversation:")
	assert.Contains(t, firstMsg.Content, "User previously asked for account cancellation.")

	require.NotNil(t, firstMsg.Metadata)
	assert.Equal(t, CompactionSummaryMetadataType, firstMsg.Metadata["type"])
	assert.Greater(t, firstMsg.Metadata["compacted_turns"].(int), 0)

	// Verify usage store recorded event with source: "compaction"
	assert.Equal(t, 1, usageStore.count())
	recorded := usageStore.recorded[0]
	assert.Equal(t, "compaction", recorded.Metadata["source"])
	assert.Equal(t, "th_compaction_test", recorded.Metadata["thread_id"])
	assert.Equal(t, 45, recorded.Usage.TotalTokens)
}

func TestApproximateTokens(t *testing.T) {
	assert.Equal(t, 0, ApproximateTokens(""))
	assert.Equal(t, 1, ApproximateTokens("hi"))
	assert.Equal(t, 3, ApproximateTokens("hello world!"))

	msgs := []core.ChatMessage{
		{Role: "user", Content: "short prompt"},
		{Role: "assistant", Content: "longer detailed response answering the prompt."},
	}
	toks := ApproximateMessageTokens(msgs)
	assert.Greater(t, toks, 10)
}
