package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestEngine_ResolveProvider(t *testing.T) {
	engine := NewEngine(EngineConfig{})

	// Default
	appDefault := &core.AIApp{
		Memory: core.MemoryConfig{
			Enabled:      true,
			ProviderType: "postgres",
		},
	}
	p, err := engine.ResolveProvider(appDefault)
	require.NoError(t, err)
	assert.Equal(t, engine.DefaultProvider(), p)

	// HTTP
	appHTTP := &core.AIApp{
		Memory: core.MemoryConfig{
			Enabled:       true,
			ProviderType:  "http",
			HTTPURL:       "https://memory.internal.net",
			HTTPAuthToken: "tok-abc",
		},
	}
	pHTTP, err := engine.ResolveProvider(appHTTP)
	require.NoError(t, err)
	assert.IsType(t, &HTTPMemoryProvider{}, pHTTP)
}

func TestEngine_PurgeSubjectData(t *testing.T) {
	ctx := context.Background()
	localMem := NewLocalMemoryProvider()
	threadStore := NewMemoryThreadStore()

	engine := NewEngine(EngineConfig{
		DefaultProvider: localMem,
		ThreadStore:     threadStore,
	})

	// Add memory
	_ = localMem.Store(ctx, core.MemoryRecord{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "usr1",
		Content:   "Secret note",
	})

	// Add thread
	_ = threadStore.CreateThread(ctx, &core.Thread{
		ID:          "th_1",
		ProjectID:   "proj1",
		Environment: "prod",
		Issuer:      "iss1",
		Subject:     "usr1",
	})

	// Purge
	err := engine.PurgeSubjectData(ctx, "proj1", "prod", "iss1", "usr1")
	require.NoError(t, err)

	// Verify memory gone
	recalled, err := localMem.Recall(ctx, core.MemoryFilter{
		ProjectID: "proj1",
		Issuer:    "iss1",
		Subject:   "usr1",
	})
	require.NoError(t, err)
	assert.Empty(t, recalled)

	// Verify thread gone
	_, err = threadStore.GetThread(ctx, "proj1", "prod", "th_1")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}
