package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestDocumentMemoryStore_ProposalWorkflowAndUndo(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryDocumentStore(nil)

	t.Run("creates proposal in pending status", func(t *testing.T) {
		prop := &core.DocumentMemoryProposal{
			ProjectID:   "p1",
			Environment: "dev",
			TargetScope: core.DocumentMemoryScopeProject,
			Key:         "company_name",
			Value:       "Acme Corp",
			Reason:      "Brand guidelines",
			ProposedBy:  "usr_alice",
		}

		err := store.CreateProposal(ctx, prop)
		require.NoError(t, err)
		assert.NotEmpty(t, prop.ID)
		assert.Equal(t, core.ProposalStatusPending, prop.Status)

		loaded, err := store.GetProposal(ctx, "p1", "dev", prop.ID)
		require.NoError(t, err)
		assert.Equal(t, "company_name", loaded.Key)
		assert.Equal(t, core.ProposalStatusPending, loaded.Status)
	})

	t.Run("rejects memory exceeding 2048 bytes", func(t *testing.T) {
		largeValue := strings.Repeat("A", 2050)
		prop := &core.DocumentMemoryProposal{
			ProjectID:   "p1",
			Environment: "dev",
			TargetScope: core.DocumentMemoryScopeApp,
			TargetID:    "app_1",
			Key:         "large_guideline",
			Value:       largeValue,
			Reason:      "Too large",
			ProposedBy:  "usr_alice",
		}

		err := store.CreateProposal(ctx, prop)
		require.ErrorIs(t, err, ErrMemoryTooLarge)
	})

	t.Run("approves proposal and maintains revision history with undo", func(t *testing.T) {
		// Proposal 1: v1
		prop1 := &core.DocumentMemoryProposal{
			ProjectID:   "p1",
			Environment: "dev",
			TargetScope: core.DocumentMemoryScopeProject,
			Key:         "support_email",
			Value:       "support@acme.com",
			Reason:      "Official contact",
			ProposedBy:  "usr_agent",
		}
		require.NoError(t, store.CreateProposal(ctx, prop1))

		mem1, err := store.ApproveProposal(ctx, "p1", "dev", prop1.ID, "admin_bob")
		require.NoError(t, err)
		assert.Equal(t, 1, mem1.Version)
		assert.Equal(t, "support@acme.com", mem1.Value)

		// Proposal 2: v2
		prop2 := &core.DocumentMemoryProposal{
			ProjectID:   "p1",
			Environment: "dev",
			TargetScope: core.DocumentMemoryScopeProject,
			Key:         "support_email",
			Value:       "helpdesk@acme.com",
			Reason:      "Updated email",
			ProposedBy:  "usr_agent",
		}
		require.NoError(t, store.CreateProposal(ctx, prop2))

		mem2, err := store.ApproveProposal(ctx, "p1", "dev", prop2.ID, "admin_bob")
		require.NoError(t, err)
		assert.Equal(t, 2, mem2.Version)
		assert.Equal(t, "helpdesk@acme.com", mem2.Value)

		// Verify current memory is v2
		current, err := store.GetMemory(ctx, "p1", "dev", core.DocumentMemoryScopeProject, "", "support_email")
		require.NoError(t, err)
		assert.Equal(t, 2, current.Version)
		assert.Equal(t, "helpdesk@acme.com", current.Value)

		// Undo: should rollback to v1
		undone, err := store.UndoMemory(ctx, "p1", "dev", core.DocumentMemoryScopeProject, "", "support_email")
		require.NoError(t, err)
		require.NotNil(t, undone)
		assert.Equal(t, 1, undone.Version)
		assert.Equal(t, "support@acme.com", undone.Value)

		// Verify current memory after undo is back to v1
		currentAfterUndo, err := store.GetMemory(ctx, "p1", "dev", core.DocumentMemoryScopeProject, "", "support_email")
		require.NoError(t, err)
		assert.Equal(t, 1, currentAfterUndo.Version)
		assert.Equal(t, "support@acme.com", currentAfterUndo.Value)
	})

	t.Run("rejects proposal with reason", func(t *testing.T) {
		prop := &core.DocumentMemoryProposal{
			ProjectID:   "p1",
			Environment: "dev",
			TargetScope: core.DocumentMemoryScopeProject,
			Key:         "bad_key",
			Value:       "bad_val",
			Reason:      "unjustified",
			ProposedBy:  "usr_agent",
		}
		require.NoError(t, store.CreateProposal(ctx, prop))

		err := store.RejectProposal(ctx, "p1", "dev", prop.ID, "admin_bob", "Violation of policy")
		require.NoError(t, err)

		loaded, err := store.GetProposal(ctx, "p1", "dev", prop.ID)
		require.NoError(t, err)
		assert.Equal(t, core.ProposalStatusRejected, loaded.Status)
		assert.Equal(t, "Violation of policy", loaded.Reason)
	})
}
