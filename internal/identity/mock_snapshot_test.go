package identity

import (
	"sync"

	"github.com/beavend/beavend/internal/core"
)

type mockSnapshotResolver struct {
	mu        sync.RWMutex
	snapshots map[string]*core.Snapshot
}

func newMockSnapshotResolver() *mockSnapshotResolver {
	return &mockSnapshotResolver{
		snapshots: make(map[string]*core.Snapshot),
	}
}

func (m *mockSnapshotResolver) Swap(snap *core.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshots[snap.ProjectID+":"+snap.Environment] = snap
}

func (m *mockSnapshotResolver) Get(projectID, env string) (*core.Snapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap, ok := m.snapshots[projectID+":"+env]
	return snap, ok
}
