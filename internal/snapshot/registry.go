package snapshot

import (
	"sync"
	"sync/atomic"

	"github.com/cortexbase/cortexbase/internal/core"
)

// Registry manages in-memory configuration snapshots across multiple projects and environments.
// Retrieval on the hot path is a lock-free, zero-allocation atomic pointer read.
type Registry struct {
	ptrs sync.Map // map[string]*atomic.Pointer[core.Snapshot]
}

// NewRegistry initializes a new Snapshot Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

func makeKey(projectID, env string) string {
	return projectID + ":" + env
}

// Get retrieves the active in-memory Snapshot for a project and environment.
// Returns (snapshot, true) if found, or (nil, false) if not registered.
// This is lock-free and allocates zero memory on the heap.
func (r *Registry) Get(projectID, env string) (*core.Snapshot, bool) {
	val, ok := r.ptrs.Load(makeKey(projectID, env))
	if !ok {
		return nil, false
	}
	ptr := val.(*atomic.Pointer[core.Snapshot])
	snap := ptr.Load()
	if snap == nil {
		return nil, false
	}
	return snap, true
}

// Swap atomically replaces the active Snapshot for the snapshot's project and environment,
// ensuring revision monotonicity (older or equal revisions are safely ignored).
func (r *Registry) Swap(snap *core.Snapshot) {
	if snap == nil {
		return
	}
	key := makeKey(snap.ProjectID, snap.Environment)
	val, loaded := r.ptrs.Load(key)
	if !loaded {
		newPtr := &atomic.Pointer[core.Snapshot]{}
		newPtr.Store(snap)
		actual, loadedActual := r.ptrs.LoadOrStore(key, newPtr)
		if !loadedActual {
			return
		}
		val = actual
	}

	ptr := val.(*atomic.Pointer[core.Snapshot])
	for {
		current := ptr.Load()
		if current != nil && current.Revision >= snap.Revision {
			// Do not downgrade to an equal or older revision
			return
		}
		if ptr.CompareAndSwap(current, snap) {
			return
		}
	}
}

// ForceSwap unconditionally replaces the active Snapshot regardless of revision number.
func (r *Registry) ForceSwap(snap *core.Snapshot) {
	if snap == nil {
		return
	}
	key := makeKey(snap.ProjectID, snap.Environment)
	val, loaded := r.ptrs.Load(key)
	if !loaded {
		newPtr := &atomic.Pointer[core.Snapshot]{}
		newPtr.Store(snap)
		actual, loadedActual := r.ptrs.LoadOrStore(key, newPtr)
		if !loadedActual {
			return
		}
		val = actual
	}

	ptr := val.(*atomic.Pointer[core.Snapshot])
	ptr.Store(snap)
}

// Delete removes a project and environment from the registry.
func (r *Registry) Delete(projectID, env string) {
	r.ptrs.Delete(makeKey(projectID, env))
}

// List returns all currently registered snapshots.
func (r *Registry) List() []*core.Snapshot {
	var list []*core.Snapshot
	r.ptrs.Range(func(_, val any) bool {
		ptr := val.(*atomic.Pointer[core.Snapshot])
		snap := ptr.Load()
		if snap != nil {
			list = append(list, snap)
		}
		return true
	})
	return list
}
