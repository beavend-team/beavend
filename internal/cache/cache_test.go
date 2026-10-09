package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestCacheKeys(t *testing.T) {
	// ResponseCacheKey
	userKey := ResponseCacheKey("proj1", "prod", "user", "sub123", "hashabc")
	assert.Equal(t, "rc:proj1:prod:user:sub123:hashabc", userKey)

	sharedKey := ResponseCacheKey("proj1", "prod", "shared", "sub123", "hashabc")
	assert.Equal(t, "rc:proj1:prod:shared:hashabc", sharedKey)

	anonKey := ResponseCacheKey("proj1", "prod", "user", "", "hashabc")
	assert.Equal(t, "rc:proj1:prod:user:anonymous:hashabc", anonKey)

	// ToolCacheKey
	toolKey := ToolCacheKey("proj1", "prod", "sub123", "get_weather", "argshash1")
	assert.Equal(t, "tc:proj1:prod:sub123:get_weather:argshash1", toolKey)

	toolAnonKey := ToolCacheKey("proj1", "prod", "", "get_weather", "argshash1")
	assert.Equal(t, "tc:proj1:prod:anonymous:get_weather:argshash1", toolAnonKey)
}

func TestComputeHashes(t *testing.T) {
	req1 := &core.ChatRequest{
		Model: "gpt-4o",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello world"},
		},
	}
	app := &core.AIApp{
		PromptVersion: 2,
	}

	hash1, err := ComputeRequestHash(req1, app)
	require.NoError(t, err)
	assert.NotEmpty(t, hash1)

	// Deterministic hash with identical inputs
	req2 := &core.ChatRequest{
		Model: "gpt-4o",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello world"},
		},
	}
	hash2, err := ComputeRequestHash(req2, app)
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2)

	// Different messages produce different hash
	req3 := &core.ChatRequest{
		Model: "gpt-4o",
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello world!"},
		},
	}
	hash3, err := ComputeRequestHash(req3, app)
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash3)

	// Tool args hash
	argsHash1 := ComputeToolArgsHash(`{"city":"Seattle","temp_unit":"c"}`)
	argsHash2 := ComputeToolArgsHash(`{"temp_unit":"c","city":"Seattle"}`)
	assert.Equal(t, argsHash1, argsHash2, "JSON order difference should produce identical normalized hash")

	argsHash3 := ComputeToolArgsHash(`{"city":"Portland","temp_unit":"c"}`)
	assert.NotEqual(t, argsHash1, argsHash3)
}

func TestMemoryCacheStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryCacheStore()

	// 1. Get non-existent
	val, err := store.Get(ctx, "non_existent")
	assert.NoError(t, err)
	assert.Nil(t, val)

	// 2. Set with TTL
	err = store.Set(ctx, "key1", []byte("hello"), 500*time.Millisecond)
	assert.NoError(t, err)

	val, err = store.Get(ctx, "key1")
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), val)

	// 3. Delete
	err = store.Delete(ctx, "key1")
	assert.NoError(t, err)

	val, err = store.Get(ctx, "key1")
	assert.NoError(t, err)
	assert.Nil(t, val)

	// 4. Expiration
	err = store.Set(ctx, "key_expire", []byte("temp"), 50*time.Millisecond)
	assert.NoError(t, err)

	time.Sleep(70 * time.Millisecond)
	val, err = store.Get(ctx, "key_expire")
	assert.NoError(t, err)
	assert.Nil(t, val, "expired key should return nil")

	// 5. DeletePattern
	_ = store.Set(ctx, "test:1", []byte("v1"), time.Minute)
	_ = store.Set(ctx, "test:2", []byte("v2"), time.Minute)
	_ = store.Set(ctx, "other:1", []byte("v3"), time.Minute)

	err = store.DeletePattern(ctx, "test:*")
	assert.NoError(t, err)

	v1, _ := store.Get(ctx, "test:1")
	v2, _ := store.Get(ctx, "test:2")
	v3, _ := store.Get(ctx, "other:1")

	assert.Nil(t, v1)
	assert.Nil(t, v2)
	assert.Equal(t, []byte("v3"), v3)

	// 6. DeleteSubjectKeys
	_ = store.Set(ctx, "rc:proj1:prod:user:user_123:hash1", []byte("resp1"), time.Minute)
	_ = store.Set(ctx, "tc:proj1:prod:user_123:toolA:argshash1", []byte("tool1"), time.Minute)
	_ = store.Set(ctx, "rc:proj1:prod:user:user_456:hash2", []byte("resp2"), time.Minute)

	err = store.DeleteSubjectKeys(ctx, "proj1", "prod", "user_123")
	assert.NoError(t, err)

	u1Resp, _ := store.Get(ctx, "rc:proj1:prod:user:user_123:hash1")
	u1Tool, _ := store.Get(ctx, "tc:proj1:prod:user_123:toolA:argshash1")
	u2Resp, _ := store.Get(ctx, "rc:proj1:prod:user:user_456:hash2")

	assert.Nil(t, u1Resp, "user_123 response cache should be deleted")
	assert.Nil(t, u1Tool, "user_123 tool cache should be deleted")
	assert.Equal(t, []byte("resp2"), u2Resp, "user_456 cache should remain intact")
}

func TestMemoryCacheStoreConcurrency(_ *testing.T) {
	ctx := context.Background()
	store := NewMemoryCacheStore()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("conc:%d", id%5)
			_ = store.Set(ctx, key, []byte(fmt.Sprintf("val-%d", id)), time.Minute)
			_, _ = store.Get(ctx, key)
		}(i)
	}

	wg.Wait()
}

func TestComputeRequestHashMultimodalAndRoutes(t *testing.T) {
	app := &core.AIApp{
		PromptVersion: 1,
	}

	reqBase := &core.ChatRequest{
		Messages: []core.ChatMessage{
			{Role: "user", Content: "Hello world"},
		},
	}

	// 1. Same prompt with different selected model (routes) gets different hash keys
	hashRouteA, err := ComputeRequestHash(reqBase, app, "model-a")
	require.NoError(t, err)
	hashRouteB, err := ComputeRequestHash(reqBase, app, "model-b")
	require.NoError(t, err)
	assert.NotEqual(t, hashRouteA, hashRouteB, "different routes must yield different cache keys")

	// 2. Same prompt with different OutputModalities gets different hash keys
	reqText := &core.ChatRequest{
		Messages:         []core.ChatMessage{{Role: "user", Content: "Generate a scene"}},
		OutputModalities: []string{"text"},
	}
	reqImage := &core.ChatRequest{
		Messages:         []core.ChatMessage{{Role: "user", Content: "Generate a scene"}},
		OutputModalities: []string{"image"},
	}
	hashText, err := ComputeRequestHash(reqText, app, "model-a")
	require.NoError(t, err)
	hashImage, err := ComputeRequestHash(reqImage, app, "model-a")
	require.NoError(t, err)
	assert.NotEqual(t, hashText, hashImage, "different output modalities must yield different cache keys")

	// 3. Same attachment bytes with different URLs gets the same key
	reqAttURL1 := &core.ChatRequest{
		Messages: []core.ChatMessage{{Role: "user", Content: "Analyze this image"}},
		Attachments: []core.Attachment{
			{
				Type:     "image",
				MimeType: "image/png",
				URL:      "https://storage.cdn1.com/images/cat.png",
				Data:     []byte("binary-image-content-bytes-12345"),
			},
		},
	}
	reqAttURL2 := &core.ChatRequest{
		Messages: []core.ChatMessage{{Role: "user", Content: "Analyze this image"}},
		Attachments: []core.Attachment{
			{
				Type:     "image",
				MimeType: "image/png",
				URL:      "https://storage.cdn2.net/signed-url/cat.png?sig=xyz987",
				Data:     []byte("binary-image-content-bytes-12345"),
			},
		},
	}
	hashAtt1, err := ComputeRequestHash(reqAttURL1, app, "model-a")
	require.NoError(t, err)
	hashAtt2, err := ComputeRequestHash(reqAttURL2, app, "model-a")
	require.NoError(t, err)
	assert.Equal(t, hashAtt1, hashAtt2, "identical attachment bytes with different URLs must produce identical cache keys")

	// 4. Different attachment bytes get different keys
	reqAttDifferent := &core.ChatRequest{
		Messages: []core.ChatMessage{{Role: "user", Content: "Analyze this image"}},
		Attachments: []core.Attachment{
			{
				Type:     "image",
				MimeType: "image/png",
				URL:      "https://storage.cdn1.com/images/cat.png",
				Data:     []byte("completely-different-image-bytes"),
			},
		},
	}
	hashAttDiff, err := ComputeRequestHash(reqAttDifferent, app, "model-a")
	require.NoError(t, err)
	assert.NotEqual(t, hashAtt1, hashAttDiff, "different attachment content must produce different cache keys")
}

func TestShouldCacheResponseModalities(t *testing.T) {
	appTextOnly := &core.AIApp{
		Cache: core.CacheConfig{
			Enabled: true,
			// Modalities is empty / default -> only text allowed
		},
	}

	appWithImage := &core.AIApp{
		Cache: core.CacheConfig{
			Enabled:    true,
			Modalities: []string{"text", "image"},
		},
	}

	reqText := &core.ChatRequest{OutputModalities: []string{"text"}}
	reqImage := &core.ChatRequest{OutputModalities: []string{"image"}}
	reqAudio := &core.ChatRequest{OutputModalities: []string{"audio"}}

	respText := &core.ChatResponse{Message: core.ChatMessage{Content: "Hello"}}
	respImage := &core.ChatResponse{
		Message: core.ChatMessage{
			ToolCalls: []core.ToolCall{
				{Name: "generate_image", Arguments: `{"prompt":"a sunset"}`},
			},
		},
	}

	// Text request is cached for text-only app
	assert.True(t, ShouldCacheResponse(reqText, respText, appTextOnly))

	// Image request or response is NOT cached when modalities does not include image
	assert.False(t, ShouldCacheResponse(reqImage, respText, appTextOnly))
	assert.False(t, ShouldCacheResponse(reqText, respImage, appTextOnly))

	// Image request is cached when app opts in to "image"
	assert.True(t, ShouldCacheResponse(reqImage, respText, appWithImage))
	assert.True(t, ShouldCacheResponse(reqText, respImage, appWithImage))

	// Audio is not enabled, so it must not be cached
	assert.False(t, ShouldCacheResponse(reqAudio, respText, appWithImage))

	// Disabled cache always returns false
	appDisabled := &core.AIApp{Cache: core.CacheConfig{Enabled: false}}
	assert.False(t, ShouldCacheResponse(reqText, respText, appDisabled))
}
