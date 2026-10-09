package media

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/core"
)

var (
	// ErrMediaNotFound indicates the requested media file does not exist.
	ErrMediaNotFound = errors.New("media: not found")
	// ErrInvalidSignature indicates the signed URL verification failed or expired.
	ErrInvalidSignature = errors.New("media: invalid or expired signature")
)

// GenerateMediaID produces a unique identifier with the "med_" prefix.
func GenerateMediaID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "med_" + hex.EncodeToString(b)
}

// ----------------------------------------------------------------------
// LocalMediaStore
// ----------------------------------------------------------------------

// LocalMediaStore implements core.MediaStore using local disk storage and HMAC signed URLs.
type LocalMediaStore struct {
	baseDir    string
	signingKey []byte
	baseURL    string
	mu         sync.RWMutex
}

// LocalMediaStoreConfig configures LocalMediaStore.
type LocalMediaStoreConfig struct {
	BaseDir    string
	SigningKey string
	BaseURL    string // e.g. "http://localhost:8080/v1/media"
}

// NewLocalMediaStore creates a new disk-backed MediaStore.
func NewLocalMediaStore(cfg LocalMediaStoreConfig) (*LocalMediaStore, error) {
	baseDir := strings.TrimSpace(cfg.BaseDir)
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "beavend_media")
	}
	if err := os.MkdirAll(baseDir, 0750); err != nil {
		return nil, fmt.Errorf("create media base dir: %w", err)
	}

	key := []byte(cfg.SigningKey)
	if len(key) == 0 {
		key = make([]byte, 32)
		_, _ = rand.Read(key)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "/v1/media"
	}

	return &LocalMediaStore{
		baseDir:    baseDir,
		signingKey: key,
		baseURL:    baseURL,
	}, nil
}

func (s *LocalMediaStore) projectDir(projectID string) string {
	return filepath.Join(s.baseDir, projectID)
}

func (s *LocalMediaStore) filePath(projectID, mediaID string) string {
	return filepath.Join(s.projectDir(projectID), mediaID)
}

func (s *LocalMediaStore) metaPath(projectID, mediaID string) string {
	return filepath.Join(s.projectDir(projectID), mediaID+".meta")
}

// Put writes media data to disk, calculates its SHA-256 digest, and saves metadata.
func (s *LocalMediaStore) Put(ctx context.Context, projectID string, data io.Reader, mimeType string, filename string) (*core.MediaRef, error) {
	if projectID == "" {
		projectID = "default"
	}
	if err := os.MkdirAll(s.projectDir(projectID), 0750); err != nil {
		return nil, fmt.Errorf("mkdir project dir: %w", err)
	}

	mediaID := GenerateMediaID()
	fPath := s.filePath(projectID, mediaID)

	tmpFile, err := os.CreateTemp(s.projectDir(projectID), "upload_*")
	if err != nil {
		return nil, fmt.Errorf("create temp media file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	written, err := io.Copy(writer, data)
	if err != nil {
		_ = tmpFile.Close()
		return nil, fmt.Errorf("copy media data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp media file: %w", err)
	}

	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	if err := os.Rename(tmpName, fPath); err != nil {
		return nil, fmt.Errorf("persist media file: %w", err)
	}

	ref := &core.MediaRef{
		MediaID:  mediaID,
		MimeType: mimeType,
		Digest:   digest,
		Filename: filename,
		Size:     written,
	}

	// Persist meta
	metaContent := fmt.Sprintf("%s\n%s\n%s\n%d\n", mimeType, digest, filename, written)
	_ = os.WriteFile(s.metaPath(projectID, mediaID), []byte(metaContent), 0600)

	// Generate signed URL
	signedURL, _ := s.SignedURL(ctx, projectID, mediaID, 24*time.Hour)
	ref.URL = signedURL

	return ref, nil
}

// Get opens the media file for reading.
func (s *LocalMediaStore) Get(ctx context.Context, projectID, mediaID string) (io.ReadCloser, *core.MediaRef, error) {
	if projectID == "" {
		projectID = "default"
	}
	fPath := s.filePath(projectID, mediaID)
	f, err := os.Open(fPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrMediaNotFound
		}
		return nil, nil, fmt.Errorf("open media file: %w", err)
	}

	ref := &core.MediaRef{
		MediaID: mediaID,
	}

	// Read meta if available
	if metaBytes, err := os.ReadFile(s.metaPath(projectID, mediaID)); err == nil {
		lines := strings.Split(strings.TrimSpace(string(metaBytes)), "\n")
		if len(lines) >= 1 {
			ref.MimeType = lines[0]
		}
		if len(lines) >= 2 {
			ref.Digest = lines[1]
		}
		if len(lines) >= 3 {
			ref.Filename = lines[2]
		}
		if len(lines) >= 4 {
			ref.Size, _ = strconv.ParseInt(lines[3], 10, 64)
		}
	}

	return f, ref, nil
}

// SignedURL generates a signed URL with HMAC verification and expiration timestamp.
func (s *LocalMediaStore) SignedURL(_ context.Context, projectID, mediaID string, ttl time.Duration) (string, error) {
	if projectID == "" {
		projectID = "default"
	}
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}
	exp := time.Now().Add(ttl).Unix()
	sig := s.sign(projectID, mediaID, exp)

	return fmt.Sprintf("%s/%s/%s?exp=%d&sig=%s", s.baseURL, url.PathEscape(projectID), url.PathEscape(mediaID), exp, sig), nil
}

// VerifySignature validates that the signed URL has not expired and matches the HMAC signature.
func (s *LocalMediaStore) VerifySignature(projectID, mediaID string, exp int64, sig string) error {
	if time.Now().Unix() > exp {
		return ErrInvalidSignature
	}
	expected := s.sign(projectID, mediaID, exp)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return ErrInvalidSignature
	}
	return nil
}

func (s *LocalMediaStore) sign(projectID, mediaID string, exp int64) string {
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(fmt.Sprintf("%s:%s:%d", projectID, mediaID, exp)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Delete removes the media file and metadata.
func (s *LocalMediaStore) Delete(_ context.Context, projectID, mediaID string) error {
	if projectID == "" {
		projectID = "default"
	}
	_ = os.Remove(s.filePath(projectID, mediaID))
	_ = os.Remove(s.metaPath(projectID, mediaID))
	return nil
}

// ----------------------------------------------------------------------
// S3MediaStore
// ----------------------------------------------------------------------

// S3MediaStore implements core.MediaStore for S3-compatible object storage (AWS S3, Cloudflare R2, MinIO)
// using standard library HTTP and AWS Signature Version 4 presigning.
type S3MediaStore struct {
	endpoint   string // e.g. "https://s3.amazonaws.com" or "http://minio:9000"
	bucket     string
	region     string
	accessKey  string
	secretKey  string
	httpClient *http.Client
}

// S3MediaStoreConfig configures S3MediaStore.
type S3MediaStoreConfig struct {
	Endpoint   string
	Bucket     string
	Region     string
	AccessKey  string
	SecretKey  string
	HTTPClient *http.Client
}

// NewS3MediaStore creates a new S3-compatible MediaStore.
func NewS3MediaStore(cfg S3MediaStoreConfig) (*S3MediaStore, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		endpoint = "https://s3.amazonaws.com"
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	return &S3MediaStore{
		endpoint:   endpoint,
		bucket:     strings.TrimSpace(cfg.Bucket),
		region:     region,
		accessKey:  strings.TrimSpace(cfg.AccessKey),
		secretKey:  strings.TrimSpace(cfg.SecretKey),
		httpClient: client,
	}, nil
}

func (s *S3MediaStore) objectKey(projectID, mediaID string) string {
	if projectID == "" {
		projectID = "default"
	}
	return fmt.Sprintf("%s/%s", projectID, mediaID)
}

// Put uploads an object to S3-compatible storage.
func (s *S3MediaStore) Put(ctx context.Context, projectID string, data io.Reader, mimeType string, filename string) (*core.MediaRef, error) {
	mediaID := GenerateMediaID()
	key := s.objectKey(projectID, mediaID)

	// Buffer or stream while computing SHA-256
	hasher := sha256.New()
	pipeReader, pipeWriter := io.Pipe()

	var written int64
	var copyErr error
	go func() {
		defer pipeWriter.Close()
		w := io.MultiWriter(pipeWriter, hasher)
		written, copyErr = io.Copy(w, data)
	}()

	urlStr := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, urlStr, pipeReader)
	if err != nil {
		return nil, fmt.Errorf("create s3 put request: %w", err)
	}
	if mimeType != "" {
		httpReq.Header.Set("Content-Type", mimeType)
	}

	// Sign request using SigV4
	s.signRequest(httpReq, time.Now().UTC())

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute s3 put: %w", err)
	}
	defer resp.Body.Close()

	if copyErr != nil {
		return nil, fmt.Errorf("read media stream: %w", copyErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("s3 put failed with status %d: %s", resp.StatusCode, string(body))
	}

	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	signedURL, _ := s.SignedURL(ctx, projectID, mediaID, 24*time.Hour)

	return &core.MediaRef{
		MediaID:  mediaID,
		MimeType: mimeType,
		Digest:   digest,
		URL:      signedURL,
		Filename: filename,
		Size:     written,
	}, nil
}

// Get downloads an object from S3-compatible storage.
func (s *S3MediaStore) Get(ctx context.Context, projectID, mediaID string) (io.ReadCloser, *core.MediaRef, error) {
	key := s.objectKey(projectID, mediaID)
	urlStr := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create s3 get request: %w", err)
	}

	s.signRequest(httpReq, time.Now().UTC())

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("execute s3 get: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, nil, ErrMediaNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("s3 get failed with status %d: %s", resp.StatusCode, string(body))
	}

	ref := &core.MediaRef{
		MediaID:  mediaID,
		MimeType: resp.Header.Get("Content-Type"),
		Size:     resp.ContentLength,
	}

	return resp.Body, ref, nil
}

// SignedURL generates a presigned GET URL for the S3 object.
func (s *S3MediaStore) SignedURL(_ context.Context, projectID, mediaID string, ttl time.Duration) (string, error) {
	key := s.objectKey(projectID, mediaID)
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}
	if ttl > 7*24*time.Hour {
		ttl = 7 * 24 * time.Hour // S3 presigned URL maximum is 7 days
	}

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	credential := fmt.Sprintf("%s/%s/%s/s3/aws4_request", s.accessKey, dateStamp, s.region)

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key))
	if err != nil {
		return "", fmt.Errorf("parse s3 endpoint url: %w", err)
	}

	q := u.Query()
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", credential)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", strconv.Itoa(int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")

	canonicalQuery := q.Encode()
	canonicalHeaders := fmt.Sprintf("host:%s\n", u.Host)
	canonicalRequest := fmt.Sprintf("GET\n%s\n%s\n%s\nhost\nUNSIGNED-PAYLOAD", u.Path, canonicalQuery, canonicalHeaders)

	hReq := sha256.Sum256([]byte(canonicalRequest))
	hReqHex := hex.EncodeToString(hReq[:])

	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s/%s/s3/aws4_request\n%s", amzDate, dateStamp, s.region, hReqHex)

	signingKey := s.deriveSigningKey(dateStamp)
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	q.Set("X-Amz-Signature", sig)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// Delete removes an object from S3.
func (s *S3MediaStore) Delete(ctx context.Context, projectID, mediaID string) error {
	key := s.objectKey(projectID, mediaID)
	urlStr := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, urlStr, nil)
	if err != nil {
		return fmt.Errorf("create s3 delete request: %w", err)
	}

	s.signRequest(httpReq, time.Now().UTC())

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("execute s3 delete: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("s3 delete failed with status %d", resp.StatusCode)
	}
	return nil
}

func (s *S3MediaStore) deriveSigningKey(dateStamp string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+s.secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(s.region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func (s *S3MediaStore) signRequest(req *http.Request, now time.Time) {
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	req.Header.Set("x-amz-date", amzDate)
	if req.Header.Get("x-amz-content-sha256") == "" {
		req.Header.Set("x-amz-content-sha256", "UNSIGNED-PAYLOAD")
	}

	// Canonical headers
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-date:%s\n", host, amzDate)
	signedHeaders := "host;x-amz-date"

	canonicalReq := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\nUNSIGNED-PAYLOAD",
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
	)

	hReq := sha256.Sum256([]byte(canonicalReq))
	hReqHex := hex.EncodeToString(hReq[:])

	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s/%s/s3/aws4_request\n%s", amzDate, dateStamp, s.region, hReqHex)
	signingKey := s.deriveSigningKey(dateStamp)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s/%s/s3/aws4_request, SignedHeaders=%s, Signature=%s",
		s.accessKey, dateStamp, s.region, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// Interface verification
var (
	_ core.MediaStore = (*LocalMediaStore)(nil)
	_ core.MediaStore = (*S3MediaStore)(nil)
)
