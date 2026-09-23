package snapshot_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cortexbase/cortexbase/internal/core"
	"github.com/cortexbase/cortexbase/internal/snapshot"
)

func TestRegistry_BasicOperations(t *testing.T) {
	reg := snapshot.NewRegistry()

	// 1. Get on non-existent returns false
	snap, ok := reg.Get("p1", "prod")
	require.False(t, ok)
	require.Nil(t, snap)

	// 2. Swap stores snapshot
	s1 := core.NewSnapshot("p1", "prod", 1)
	reg.Swap(s1)

	snap, ok = reg.Get("p1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(1), snap.Revision)

	// 3. Swap with higher revision
	s2 := core.NewSnapshot("p1", "prod", 2)
	reg.Swap(s2)

	snap, ok = reg.Get("p1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(2), snap.Revision)

	// 4. Swap with lower revision does not downgrade
	sLower := core.NewSnapshot("p1", "prod", 1)
	reg.Swap(sLower)
	snap, ok = reg.Get("p1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(2), snap.Revision, "should not downgrade from revision 2 to 1")

	// 5. ForceSwap allows override
	reg.ForceSwap(sLower)
	snap, ok = reg.Get("p1", "prod")
	require.True(t, ok)
	require.Equal(t, int64(1), snap.Revision, "ForceSwap must allow explicit downgrade")

	// 6. Delete
	reg.Delete("p1", "prod")
	snap, ok = reg.Get("p1", "prod")
	require.False(t, ok)
	require.Nil(t, snap)
}

func TestRegistry_ConcurrentReadsAndWrites(t *testing.T) {
	reg := snapshot.NewRegistry()
	projectID := "proj-concurrent"
	env := "production"

	initial := core.NewSnapshot(projectID, env, 1)
	reg.Swap(initial)

	const readers = 16
	const iterations = 500

	var wg sync.WaitGroup
	wg.Add(readers + 1)

	// Concurrent writer continuously updating snapshots
	go func() {
		defer wg.Done()
		for i := int64(2); i <= iterations; i++ {
			snap := core.NewSnapshot(projectID, env, i)
			reg.Swap(snap)
			time.Sleep(10 * time.Microsecond)
		}
	}()

	// Concurrent readers holding references
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			var prevRev int64
			for i := 0; i < iterations; i++ {
				snap, ok := reg.Get(projectID, env)
				require.True(t, ok)
				require.NotNil(t, snap)
				require.GreaterOrEqual(t, snap.Revision, prevRev)
				prevRev = snap.Revision
				// In-flight request simulation: read snap properties
				_ = snap.ProjectID
				_ = snap.Environment
			}
		}()
	}

	wg.Wait()
}

func TestRegistry_InFlightReferenceIsolation(t *testing.T) {
	reg := snapshot.NewRegistry()
	projectID := "proj-isolation"
	env := "staging"

	s1 := core.NewSnapshot(projectID, env, 1)
	s1.Apps["app1"] = &core.AIApp{ID: "app1", Name: "Version 1"}
	reg.Swap(s1)

	// In-flight request acquires s1
	inFlightSnap, ok := reg.Get(projectID, env)
	require.True(t, ok)
	require.Equal(t, int64(1), inFlightSnap.Revision)
	require.Equal(t, "Version 1", inFlightSnap.Apps["app1"].Name)

	// Control plane swaps s2
	s2 := core.NewSnapshot(projectID, env, 2)
	s2.Apps["app1"] = &core.AIApp{ID: "app1", Name: "Version 2"}
	reg.Swap(s2)

	// Registry now returns s2
	currentSnap, ok := reg.Get(projectID, env)
	require.True(t, ok)
	require.Equal(t, int64(2), currentSnap.Revision)
	require.Equal(t, "Version 2", currentSnap.Apps["app1"].Name)

	// But in-flight request holding inFlightSnap is completely unaffected
	require.Equal(t, int64(1), inFlightSnap.Revision)
	require.Equal(t, "Version 1", inFlightSnap.Apps["app1"].Name)
}
