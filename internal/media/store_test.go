package media_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/media"
)

func TestLocalMediaStore_PutGetDelete(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "beavend_media_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	store, err := media.NewLocalMediaStore(media.LocalMediaStoreConfig{
		BaseDir:    tempDir,
		SigningKey: "secret-key-123",
		BaseURL:    "https://api.beavend.io/v1/media",
	})
	require.NoError(t, err)

	content := []byte("fake image data for testing")
	expectedDigest := "sha256:" + hex.EncodeToString(func() []byte {
		h := sha256.Sum256(content)
		return h[:]
	}())

	// 1. Put
	ref, err := store.Put(ctx, "proj_1", bytes.NewReader(content), "image/png", "sample.png")
	require.NoError(t, err)
	require.NotEmpty(t, ref.MediaID)
	assert.Equal(t, "image/png", ref.MimeType)
	assert.Equal(t, expectedDigest, ref.Digest)
	assert.Equal(t, "sample.png", ref.Filename)
	assert.Equal(t, int64(len(content)), ref.Size)
	assert.Contains(t, ref.URL, "https://api.beavend.io/v1/media/proj_1/"+ref.MediaID)
	assert.Contains(t, ref.URL, "sig=")

	// 2. Get
	reader, gotRef, err := store.Get(ctx, "proj_1", ref.MediaID)
	require.NoError(t, err)
	defer reader.Close()

	readBytes, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, readBytes)
	assert.Equal(t, "image/png", gotRef.MimeType)
	assert.Equal(t, expectedDigest, gotRef.Digest)
	assert.Equal(t, "sample.png", gotRef.Filename)
	assert.Equal(t, int64(len(content)), gotRef.Size)

	// 3. SignedURL verification
	signedURL, err := store.SignedURL(ctx, "proj_1", ref.MediaID, 1*time.Hour)
	require.NoError(t, err)
	assert.Contains(t, signedURL, ref.MediaID)

	// 4. Delete
	err = store.Delete(ctx, "proj_1", ref.MediaID)
	require.NoError(t, err)

	// 5. Get after delete should return ErrMediaNotFound
	_, _, err = store.Get(ctx, "proj_1", ref.MediaID)
	require.ErrorIs(t, err, media.ErrMediaNotFound)
}

func TestS3MediaStore_MockPutGet(t *testing.T) {
	ctx := context.Background()
	content := []byte("s3 test content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "host;x-amz-date", r.Header.Get("Authorization")[stringsIndex(r.Header.Get("Authorization"), "SignedHeaders=")+14:stringsIndex(r.Header.Get("Authorization"), ", Signature=")])

		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	s3Store, err := media.NewS3MediaStore(media.S3MediaStoreConfig{
		Endpoint:   server.URL,
		Bucket:     "test-bucket",
		Region:     "us-west-2",
		AccessKey:  "AKIAIOSFODNN7EXAMPLE",
		SecretKey:  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		HTTPClient: server.Client(),
	})
	require.NoError(t, err)

	ref, err := s3Store.Put(ctx, "proj_s3", bytes.NewReader(content), "image/jpeg", "photo.jpg")
	require.NoError(t, err)
	require.NotEmpty(t, ref.MediaID)
	assert.Equal(t, "image/jpeg", ref.MimeType)
	assert.Contains(t, ref.URL, server.URL+"/test-bucket/proj_s3/"+ref.MediaID)
	assert.Contains(t, ref.URL, "X-Amz-Signature=")

	rc, gotRef, err := s3Store.Get(ctx, "proj_s3", ref.MediaID)
	require.NoError(t, err)
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	assert.Equal(t, content, data)
	assert.Equal(t, "image/jpeg", gotRef.MimeType)

	err = s3Store.Delete(ctx, "proj_s3", ref.MediaID)
	require.NoError(t, err)
}

func stringsIndex(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
