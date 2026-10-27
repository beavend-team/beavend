package tools

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/beavend/beavend/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalValidWASM is the standard 8-byte WebAssembly binary header.
var minimalValidWASM = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func TestWASMRuntime_CompilationCaching(t *testing.T) {
	ctx := context.Background()

	rt, err := NewWASMRuntime(ctx, nil, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	// First compilation
	cm1, err := rt.Compile(ctx, minimalValidWASM)
	require.NoError(t, err)
	require.NotNil(t, cm1)

	// Second compilation of identical bytecode must hit cache
	cm2, err := rt.Compile(ctx, minimalValidWASM)
	require.NoError(t, err)
	assert.Same(t, cm1, cm2, "expected identical compiled module pointer from cache")
}

func TestWASMKVStore_QuotaAndLimits(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryWASMKVStore()

	// 1. Put and Get
	err := store.Put(ctx, "tenant_1", "greeting", []byte("hello world"))
	require.NoError(t, err)

	val, err := store.Get(ctx, "tenant_1", "greeting")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello world"), val)

	// Tenant isolation
	val2, err := store.Get(ctx, "tenant_2", "greeting")
	require.NoError(t, err)
	assert.Nil(t, val2)

	// 2. Key too large (>64 KB)
	largeKey := strings.Repeat("k", MaxWASMKeyBytes+1)
	err = store.Put(ctx, "tenant_1", largeKey, []byte("val"))
	require.ErrorIs(t, err, ErrWASMKeyTooLarge)

	// 3. Value too large (>64 KB)
	largeVal := make([]byte, MaxWASMKeyBytes+1)
	err = store.Put(ctx, "tenant_1", "normal_key", largeVal)
	require.ErrorIs(t, err, ErrWASMValTooLarge)
}

func TestWASMExecutor_ExecutionAndSemaphore(t *testing.T) {
	ctx := context.Background()

	rt, err := NewWASMRuntime(ctx, nil, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	wasmB64 := base64.StdEncoding.EncodeToString(minimalValidWASM)

	toolDef := core.ToolDefinition{
		ID:   "wasm_tool_1",
		Name: "wasm_test",
		Executor: core.ExecutorConfig{
			Type: "wasm",
			WASM: core.WASMConfig{
				BinaryBase64: wasmB64,
				MaxMemoryMB:  16,
			},
			TimeoutMS: 5000,
		},
	}

	exec := NewWASMExecutor(toolDef, rt)

	// Single execution
	res, err := exec.Execute(ctx, core.ToolCall{ID: "c1", Name: "wasm_test", Arguments: `{"x":1}`}, core.Identity{})
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.Equal(t, "wazero", res.Metadata["runtime"])
	assert.Equal(t, 16, res.Metadata["memory_max_mb"])

	// Concurrency test: execute multiple in parallel
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, e := exec.Execute(ctx, core.ToolCall{ID: "c_par", Name: "wasm_test", Arguments: `{}`}, core.Identity{})
			if e != nil {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, 0, errCount)
}

func TestWASMExecutor_HostABI_HTTP(t *testing.T) {
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = r.Body.Close()
		_, _ = w.Write([]byte(`{"result":"ok_from_server"}`))
	}))
	defer ts.Close()

	// Verify that the safe client configured in WASMRuntime can execute requests through host ABI
	client := ts.Client()
	store := NewMemoryWASMKVStore()

	rt, err := NewWASMRuntime(ctx, store, client)
	require.NoError(t, err)
	defer rt.Close(ctx)

	// Test store integration with tenant
	err = store.Put(ctx, "default", "config_val", []byte("12345"))
	require.NoError(t, err)
	got, err := store.Get(ctx, "default", "config_val")
	require.NoError(t, err)
	assert.Equal(t, "12345", string(got))
}

func TestWASMHostABI_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryWASMKVStore()

	// Tenant A context
	snapA := &core.Snapshot{
		ProjectID:   "tenant-alpha",
		Environment: "prod",
	}
	ctxA := core.ContextWithSnapshot(ctx, snapA)
	ctxA = core.ContextWithAppID(ctxA, "app-alpha")

	// Tenant B context
	snapB := &core.Snapshot{
		ProjectID:   "tenant-bravo",
		Environment: "prod",
	}
	ctxB := core.ContextWithSnapshot(ctx, snapB)
	ctxB = core.ContextWithAppID(ctxB, "app-bravo")

	keyA := tenantKeyFromContext(ctxA)
	keyB := tenantKeyFromContext(ctxB)
	assert.Equal(t, "tenant-alpha:prod:app-alpha", keyA)
	assert.Equal(t, "tenant-bravo:prod:app-bravo", keyB)
	assert.NotEqual(t, keyA, keyB)

	// Write key under Tenant A
	err := store.Put(ctxA, keyA, "app_secret", []byte("alpha-confidential"))
	require.NoError(t, err)

	// Write same key name under Tenant B
	err = store.Put(ctxB, keyB, "app_secret", []byte("bravo-confidential"))
	require.NoError(t, err)

	// Read Tenant A's key
	valA, err := store.Get(ctxA, keyA, "app_secret")
	require.NoError(t, err)
	assert.Equal(t, "alpha-confidential", string(valA))

	// Read Tenant B's key - must be isolated from Tenant A
	valB, err := store.Get(ctxB, keyB, "app_secret")
	require.NoError(t, err)
	assert.Equal(t, "bravo-confidential", string(valB))

	// Tenant B cannot see Tenant A's data
	valCross, err := store.Get(ctxB, keyB, "only_in_alpha")
	require.NoError(t, err)
	assert.Nil(t, valCross)
}

func TestWASMRuntime_DefaultClientPortRestriction(t *testing.T) {
	ctx := context.Background()

	rt, err := NewWASMRuntime(ctx, nil, nil)
	require.NoError(t, err)
	defer rt.Close(ctx)

	// Default HTTP client must have SafeDialer configured with ports 80/443
	require.NotNil(t, rt.httpClient)
	transport, ok := rt.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialContext)

	// Connecting to an unapproved port (e.g., 22 SSH or 6379 Redis) must fail with port restriction error
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://1.1.1.1:6379", nil)
	require.NoError(t, err)
	_, err = rt.httpClient.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowed ports")
}
