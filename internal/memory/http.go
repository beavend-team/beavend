package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
)

// HTTPMemoryProviderConfig specifies configuration for the implementor-hosted memory provider.
type HTTPMemoryProviderConfig struct {
	EndpointURL string        `json:"endpoint_url"`
	AuthToken   string        `json:"auth_token,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	Client      *http.Client  `json:"-"`
}

// HTTPMemoryProvider delegates memory storage, recall, and search to an implementor-hosted HTTP API.
type HTTPMemoryProvider struct {
	endpointURL string
	authToken   string
	client      *http.Client
}

// NewHTTPMemoryProvider initializes an implementor-hosted HTTP memory provider.
func NewHTTPMemoryProvider(cfg HTTPMemoryProviderConfig) (*HTTPMemoryProvider, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.EndpointURL), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("http memory provider: empty endpoint URL")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := cfg.Client
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
		}
	}

	return &HTTPMemoryProvider{
		endpointURL: endpoint,
		authToken:   cfg.AuthToken,
		client:      client,
	}, nil
}

// Store delegates memory storage to POST {endpoint}/store.
func (p *HTTPMemoryProvider) Store(ctx context.Context, record core.MemoryRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("http memory store: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL+"/store", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http memory store: build request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("http memory store: execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("http memory store: status %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

// Recall delegates memory retrieval to POST {endpoint}/recall.
func (p *HTTPMemoryProvider) Recall(ctx context.Context, filter core.MemoryFilter) ([]core.MemoryRecord, error) {
	body, err := json.Marshal(filter)
	if err != nil {
		return nil, fmt.Errorf("http memory recall: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL+"/recall", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http memory recall: build request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http memory recall: execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("http memory recall: status %d: %s", resp.StatusCode, string(b))
	}

	var records []core.MemoryRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("http memory recall: decode response: %w", err)
	}

	return records, nil
}

// Search delegates vector/query search to POST {endpoint}/search.
func (p *HTTPMemoryProvider) Search(ctx context.Context, query core.MemorySearchQuery) ([]core.MemorySearchResult, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("http memory search: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL+"/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http memory search: build request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http memory search: execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("http memory search: status %d: %s", resp.StatusCode, string(b))
	}

	var results []core.MemorySearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("http memory search: decode response: %w", err)
	}

	return results, nil
}

// DeleteBySubject delegates subject memory deletion to POST {endpoint}/delete-by-subject.
func (p *HTTPMemoryProvider) DeleteBySubject(ctx context.Context, projectID, issuer, subject string) error {
	payload := map[string]string{
		"project_id": projectID,
		"issuer":     issuer,
		"subject":    subject,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL+"/delete-by-subject", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http memory delete: build request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("http memory delete: execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("http memory delete: status %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

func (p *HTTPMemoryProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.authToken != "" {
		if strings.HasPrefix(strings.ToLower(p.authToken), "bearer ") {
			req.Header.Set("Authorization", p.authToken)
		} else {
			req.Header.Set("Authorization", "Bearer "+p.authToken)
		}
	}
}
