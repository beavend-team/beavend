package audit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryAuditor(t *testing.T) {
	auditor := NewInMemoryAuditor()
	ctx := context.Background()

	err := auditor.Record(ctx, Entry{
		ProjectID:    "proj_1",
		Environment:  "production",
		UserID:       "usr_admin",
		Action:       "create_app",
		ResourceType: "ai_app",
		ResourceID:   "app_chat",
		Payload:      map[string]any{"name": "Chatbot"},
	})
	require.NoError(t, err)

	entries := auditor.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "proj_1", entries[0].ProjectID)
	assert.Equal(t, "create_app", entries[0].Action)
	assert.Equal(t, "ai_app", entries[0].ResourceType)
	assert.Equal(t, "app_chat", entries[0].ResourceID)
	assert.False(t, entries[0].Timestamp.IsZero())
}

func TestStoreAuditor_NilQueries(t *testing.T) {
	auditor := NewStoreAuditor(nil)
	err := auditor.Record(context.Background(), Entry{
		Action: "noop",
	})
	assert.NoError(t, err)
}
