package snapshot

import (
	"context"

	"github.com/beavend/beavend/internal/core"
)

// ContextWithSnapshot returns a new context containing the Snapshot.
func ContextWithSnapshot(ctx context.Context, snap *core.Snapshot) context.Context {
	return core.ContextWithSnapshot(ctx, snap)
}

// FromContext extracts the Snapshot from context if present.
func FromContext(ctx context.Context) (*core.Snapshot, bool) {
	return core.SnapshotFromContext(ctx)
}
