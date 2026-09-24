package snapshot_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/snapshot"
)

func TestSnapshotContext(t *testing.T) {
	snap := core.NewSnapshot("proj-1", "prod", 1)

	ctx := context.Background()

	// 1. Not in context
	got, ok := snapshot.FromContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, got)

	// 2. Injected into context
	ctxWithSnap := snapshot.ContextWithSnapshot(ctx, snap)
	got, ok = snapshot.FromContext(ctxWithSnap)
	assert.True(t, ok)
	assert.Equal(t, snap, got)

	// 3. Nil context
	var nilCtx context.Context
	got, ok = snapshot.FromContext(nilCtx)
	assert.False(t, ok)
	assert.Nil(t, got)
}
