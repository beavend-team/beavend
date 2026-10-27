package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/core"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	// MaxConcurrentWASM is the maximum number of concurrent WASM executions allowed per instance.
	MaxConcurrentWASM = 32

	// DefaultWASMMaxMemoryMB is the default maximum memory limit for sandboxed modules (16 MB).
	DefaultWASMMaxMemoryMB = 16

	// MaxWASMKeyBytes is the maximum allowed size (64 KB) for a single KV key or value.
	MaxWASMKeyBytes = 64 * 1024

	// MaxWASMAppKVBytes is the maximum aggregate storage per app in the KV store (10 MB).
	MaxWASMAppKVBytes = 10 * 1024 * 1024
)

var (
	ErrWASMExecution  = errors.New("wasm execution error")
	ErrWASMMemoryLimit = errors.New("wasm memory limit exceeded")
	ErrWASMKeyTooLarge = errors.New("wasm kv key exceeds maximum allowed size")
	ErrWASMValTooLarge = errors.New("wasm kv value exceeds maximum allowed size")
	ErrWASMQuotaExceeded = errors.New("wasm kv storage quota exceeded for application")
)

// WASMKVStore defines tenant-scoped key-value storage accessible from sandboxed WASM modules.
type WASMKVStore interface {
	Get(ctx context.Context, tenantKey, key string) ([]byte, error)
	Put(ctx context.Context, tenantKey, key string, val []byte) error
}

// MemoryWASMKVStore is a thread-safe in-memory implementation of WASMKVStore with quota enforcement.
type MemoryWASMKVStore struct {
	mu    sync.RWMutex
	store map[string]map[string][]byte
	usage map[string]int
}

// NewMemoryWASMKVStore creates a new MemoryWASMKVStore.
func NewMemoryWASMKVStore() *MemoryWASMKVStore {
	return &MemoryWASMKVStore{
		store: make(map[string]map[string][]byte),
		usage: make(map[string]int),
	}
}

func (s *MemoryWASMKVStore) Get(_ context.Context, tenantKey, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenant, ok := s.store[tenantKey]
	if !ok {
		return nil, nil
	}
	val, ok := tenant[key]
	if !ok {
		return nil, nil
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	return cp, nil
}

func (s *MemoryWASMKVStore) Put(_ context.Context, tenantKey, key string, val []byte) error {
	if len(key) > MaxWASMKeyBytes {
		return ErrWASMKeyTooLarge
	}
	if len(val) > MaxWASMKeyBytes {
		return ErrWASMValTooLarge
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tenant, ok := s.store[tenantKey]
	if !ok {
		tenant = make(map[string][]byte)
		s.store[tenantKey] = tenant
	}

	oldLen := len(tenant[key])
	newTotal := s.usage[tenantKey] - oldLen + len(val)
	if newTotal > MaxWASMAppKVBytes {
		return ErrWASMQuotaExceeded
	}

	cp := make([]byte, len(val))
	copy(cp, val)
	tenant[key] = cp
	s.usage[tenantKey] = newTotal
	return nil
}

// WASMRuntime manages module compilation caching, bounded execution slots, and host ABI integration.
type WASMRuntime struct {
	runtime    wazero.Runtime
	mu         sync.RWMutex
	compiled   map[string]wazero.CompiledModule
	sem        chan struct{}
	kvStore    WASMKVStore
	httpClient *http.Client
}

// NewWASMRuntime creates a new WASMRuntime with wazero and host ABI registered.
func NewWASMRuntime(ctx context.Context, kvStore WASMKVStore, httpClient *http.Client) (*WASMRuntime, error) {
	if kvStore == nil {
		kvStore = NewMemoryWASMKVStore()
	}
	if httpClient == nil {
		httpClient = NewSafeHTTPClient(SafeDialerConfig{
			AllowedPorts: []uint16{80, 443},
		}, 15*time.Second)
	}

	rConfig := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(DefaultWASMMaxMemoryMB * 16). // 16 pages per MB -> 256 pages = 16MB
		WithCloseOnContextDone(true)
	r := wazero.NewRuntimeWithConfig(ctx, rConfig)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	wr := &WASMRuntime{
		runtime:    r,
		compiled:   make(map[string]wazero.CompiledModule),
		sem:        make(chan struct{}, MaxConcurrentWASM),
		kvStore:    kvStore,
		httpClient: httpClient,
	}

	if err := wr.registerHostABI(ctx); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("register host abi: %w", err)
	}

	return wr, nil
}

func tenantKeyFromContext(ctx context.Context) string {
	snap, ok := core.SnapshotFromContext(ctx)
	appID, _ := core.AppIDFromContext(ctx)
	if ok && snap != nil && snap.ProjectID != "" {
		return fmt.Sprintf("%s:%s:%s", snap.ProjectID, snap.Environment, appID)
	}
	if appID != "" {
		return appID
	}
	return "default"
}

// registerHostABI registers "env" functions: beavend_kv_get, beavend_kv_put, and beavend_http_request.
func (wr *WASMRuntime) registerHostABI(ctx context.Context) error {
	_, err := wr.runtime.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(callCtx context.Context, m api.Module, keyPtr, keyLen, valBufPtr, valBufLen uint32) int32 {
			mem := m.Memory()
			keyBytes, ok := mem.Read(keyPtr, keyLen)
			if !ok {
				return -1
			}

			tenantKey := tenantKeyFromContext(callCtx)
			val, err := wr.kvStore.Get(callCtx, tenantKey, string(keyBytes))
			if err != nil || val == nil {
				return -1
			}

			n := uint32(len(val))
			if n > valBufLen {
				n = valBufLen
			}
			if !mem.Write(valBufPtr, val[:n]) {
				return -1
			}
			return int32(len(val))
		}).
		Export("beavend_kv_get").
		NewFunctionBuilder().
		WithFunc(func(callCtx context.Context, m api.Module, keyPtr, keyLen, valPtr, valLen uint32) int32 {
			mem := m.Memory()
			keyBytes, ok := mem.Read(keyPtr, keyLen)
			if !ok {
				return -1
			}
			valBytes, ok := mem.Read(valPtr, valLen)
			if !ok {
				return -1
			}

			tenantKey := tenantKeyFromContext(callCtx)
			if err := wr.kvStore.Put(callCtx, tenantKey, string(keyBytes), valBytes); err != nil {
				return -1
			}
			return 0
		}).
		Export("beavend_kv_put").
		NewFunctionBuilder().
		WithFunc(func(callCtx context.Context, m api.Module, reqPtr, reqLen, respBufPtr, respBufLen uint32) int32 {
			mem := m.Memory()
			reqBytes, ok := mem.Read(reqPtr, reqLen)
			if !ok {
				return -1
			}

			var httpReq struct {
				Method  string            `json:"method"`
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
				Body    string            `json:"body"`
			}
			if err := json.Unmarshal(reqBytes, &httpReq); err != nil {
				return -1
			}

			method := strings.ToUpper(strings.TrimSpace(httpReq.Method))
			if method == "" {
				method = http.MethodGet
			}

			hr, err := http.NewRequestWithContext(callCtx, method, httpReq.URL, strings.NewReader(httpReq.Body))
			if err != nil {
				return -1
			}
			for k, v := range httpReq.Headers {
				hr.Header.Set(k, v)
			}

			resp, err := wr.httpClient.Do(hr)
			if err != nil {
				return -1
			}
			defer resp.Body.Close()

			bodyData, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resMap := map[string]any{
				"status": resp.StatusCode,
				"body":   string(bodyData),
			}
			outBytes, _ := json.Marshal(resMap)
			n := uint32(len(outBytes))
			if n > respBufLen {
				n = respBufLen
			}
			if !mem.Write(respBufPtr, outBytes[:n]) {
				return -1
			}
			return int32(len(outBytes))
		}).
		Export("beavend_http_request").
		Instantiate(ctx)

	return err
}

// Close closes the underlying wazero runtime.
func (wr *WASMRuntime) Close(ctx context.Context) error {
	return wr.runtime.Close(ctx)
}

// Compile compiles the WASM bytecode, caching it by SHA-256 hash.
func (wr *WASMRuntime) Compile(ctx context.Context, wasmBytes []byte) (wazero.CompiledModule, error) {
	h := sha256.Sum256(wasmBytes)
	key := hex.EncodeToString(h[:])

	wr.mu.RLock()
	cm, ok := wr.compiled[key]
	wr.mu.RUnlock()
	if ok {
		return cm, nil
	}

	wr.mu.Lock()
	defer wr.mu.Unlock()

	// Double-check under lock
	if cm, ok = wr.compiled[key]; ok {
		return cm, nil
	}

	compiled, err := wr.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}
	wr.compiled[key] = compiled
	return compiled, nil
}

// WASMExecutor implements core.Executor for sandboxed WebAssembly execution.
type WASMExecutor struct {
	def     core.ToolDefinition
	runtime *WASMRuntime
}

// NewWASMExecutor creates a new WASMExecutor for the specified tool definition.
func NewWASMExecutor(def core.ToolDefinition, runtime *WASMRuntime) *WASMExecutor {
	return &WASMExecutor{
		def:     def,
		runtime: runtime,
	}
}

// Execute instantiates a fresh module in an isolated slot, runs it, and returns sanitized result.
func (e *WASMExecutor) Execute(ctx context.Context, call core.ToolCall, id core.Identity) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: err.Error(),
			IsError: true,
		}, err
	}

	timeout := time.Duration(e.def.Executor.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 1. Acquire bounded execution slot (semaphore capacity = MaxConcurrentWASM)
	select {
	case e.runtime.sem <- struct{}{}:
		defer func() { <-e.runtime.sem }()
	case <-execCtx.Done():
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: "wasm concurrency limit exceeded or execution timed out",
			IsError: true,
		}, execCtx.Err()
	}

	// 2. Resolve WASM bytecode
	var wasmBytes []byte
	var err error
	if b64 := e.def.Executor.WASM.BinaryBase64; b64 != "" {
		wasmBytes, err = base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return core.ToolResult{
				CallID:  call.ID,
				ToolID:  e.def.ID,
				Content: fmt.Sprintf("invalid wasm binary base64: %v", err),
				IsError: true,
			}, err
		}
	} else if u := e.def.Executor.WASM.BinaryURL; u != "" {
		req, reqErr := http.NewRequestWithContext(execCtx, http.MethodGet, u, nil)
		if reqErr != nil {
			return core.ToolResult{CallID: call.ID, ToolID: e.def.ID, Content: reqErr.Error(), IsError: true}, reqErr
		}
		resp, respErr := e.runtime.httpClient.Do(req)
		if respErr != nil {
			return core.ToolResult{CallID: call.ID, ToolID: e.def.ID, Content: respErr.Error(), IsError: true}, respErr
		}
		defer resp.Body.Close()
		wasmBytes, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16MB max binary
		if err != nil {
			return core.ToolResult{CallID: call.ID, ToolID: e.def.ID, Content: err.Error(), IsError: true}, err
		}
	} else {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: "wasm binary_base64 or binary_url is required",
			IsError: true,
		}, fmt.Errorf("%w: missing wasm binary", ErrWASMExecution)
	}

	// 3. Compile or get cached module
	compiled, err := e.runtime.Compile(execCtx, wasmBytes)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("failed to compile wasm module: %v", err),
			IsError: true,
		}, err
	}

	// 4. Configure module sandbox limits
	maxMemMB := e.def.Executor.WASM.MaxMemoryMB
	if maxMemMB <= 0 {
		maxMemMB = DefaultWASMMaxMemoryMB
	}

	var stdout, stderr bytes.Buffer
	argsData := []byte(call.Arguments)
	if len(argsData) == 0 {
		argsData = []byte("{}")
	}

	modConfig := wazero.NewModuleConfig().
		WithStdin(bytes.NewReader(argsData)).
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithArgs(call.Name, call.Arguments)

	// 5. Instantiate fresh module instance for this call
	mod, err := e.runtime.runtime.InstantiateModule(execCtx, compiled, modConfig)
	if err != nil {
		errStr := strings.TrimSpace(stderr.String())
		if errStr == "" {
			errStr = err.Error()
		}
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  e.def.ID,
			Content: fmt.Sprintf("wasm execution failed: %s", errStr),
			IsError: true,
		}, err
	}
	defer mod.Close(execCtx)

	// Check if a specific custom entrypoint function is defined and exported
	entrypoint := e.def.Executor.WASM.Entrypoint
	if entrypoint != "" && entrypoint != "_start" {
		fn := mod.ExportedFunction(entrypoint)
		if fn != nil {
			_, err = fn.Call(execCtx)
			if err != nil {
				return core.ToolResult{
					CallID:  call.ID,
					ToolID:  e.def.ID,
					Content: fmt.Sprintf("wasm entrypoint %s error: %v", entrypoint, err),
					IsError: true,
				}, err
			}
		}
	}

	outContent := strings.TrimSpace(stdout.String())
	if outContent == "" {
		outContent = "{}"
	}

	return core.ToolResult{
		CallID:  call.ID,
		ToolID:  e.def.ID,
		Content: outContent,
		Metadata: map[string]any{
			"runtime":       "wazero",
			"memory_max_mb": maxMemMB,
		},
	}, nil
}
