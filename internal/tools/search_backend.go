package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SearchBackend defines the common interface for pluggable search engines.
type SearchBackend interface {
	Name() string
	Search(ctx context.Context, params SearchParams) (*SearXNGSearchResult, error)
}

// SearchParams contains the criteria, tenant context, and security policy for a search query.
type SearchParams struct {
	Query      string            `json:"query"`
	AIAppID    string            `json:"ai_app_id"`
	ProjectID  string            `json:"project_id"`
	Env        string            `json:"env"`
	SafeSearch int               `json:"safe_search"` // 0=off, 1=moderate, 2=strict
	MaxResults int               `json:"max_results"`
	Categories []string          `json:"categories,omitempty"`
	Engines    []string          `json:"engines,omitempty"`
	TimeRange  string            `json:"time_range,omitempty"`
	BaseURL    string            `json:"base_url,omitempty"`
	APIKey     string            `json:"api_key,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

// ComputeSearchCacheKey produces a scoped SHA-256 hash including security policies and tenant identifiers
// to prevent cross-scope cache poisoning attacks.
func ComputeSearchCacheKey(p SearchParams) string {
	h := sha256.New()

	cats := make([]string, len(p.Categories))
	copy(cats, p.Categories)
	sort.Strings(cats)

	engs := make([]string, len(p.Engines))
	copy(engs, p.Engines)
	sort.Strings(engs)

	fmt.Fprintf(h, "app:%s|proj:%s|env:%s|q:%s|safe:%d|max:%d|cat:%s|eng:%s|time:%s",
		strings.TrimSpace(p.AIAppID),
		strings.TrimSpace(p.ProjectID),
		strings.TrimSpace(p.Env),
		strings.TrimSpace(strings.ToLower(p.Query)),
		p.SafeSearch,
		p.MaxResults,
		strings.Join(cats, ","),
		strings.Join(engs, ","),
		strings.TrimSpace(p.TimeRange),
	)

	return hex.EncodeToString(h.Sum(nil))
}

// NewSearchBackend instantiates a SearchBackend implementation based on the specified backend name.
// Defaults to SearXNGBackend if name is unrecognized or empty.
func NewSearchBackend(backendName string, client *http.Client) SearchBackend {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	switch strings.ToLower(strings.TrimSpace(backendName)) {
	case "exa":
		return &ExaSearchBackend{client: client}
	case "firecrawl":
		return &FirecrawlSearchBackend{client: client}
	case "serply":
		return &SerplySearchBackend{client: client}
	case "http_json":
		return &HTTPJSONSearchBackend{client: client}
	case "searxng":
		fallthrough
	default:
		return &SearXNGSearchBackend{client: client}
	}
}

// SearXNGSearchBackend delegates queries to a SearXNG instance.
type SearXNGSearchBackend struct {
	client *http.Client
}

func (b *SearXNGSearchBackend) Name() string { return "searxng" }

func (b *SearXNGSearchBackend) Search(ctx context.Context, p SearchParams) (*SearXNGSearchResult, error) {
	searxClient := NewSearXNGClient(p.BaseURL, true, 10*time.Second, b.client)
	return searxClient.Search(ctx, p.Query, p.Categories, p.Engines, p.TimeRange, p.SafeSearch, p.MaxResults)
}

// ExaSearchBackend integrates with the Exa (Metaphor) search API.
type ExaSearchBackend struct {
	client *http.Client
}

func (b *ExaSearchBackend) Name() string { return "exa" }

func (b *ExaSearchBackend) Search(ctx context.Context, p SearchParams) (*SearXNGSearchResult, error) {
	endpoint := "https://api.exa.ai/search"
	if p.BaseURL != "" {
		endpoint = strings.TrimRight(p.BaseURL, "/") + "/search"
	}

	maxResults := p.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}

	reqBody := map[string]any{
		"query":      p.Query,
		"numResults": maxResults,
		"useAutoprompt": true,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("x-api-key", p.APIKey)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: "exa_http_error",
			Message:   fmt.Sprintf("Exa returned HTTP %d", resp.StatusCode),
		}, nil
	}

	var exaResp struct {
		Results []struct {
			Title string  `json:"title"`
			URL   string  `json:"url"`
			Text  string  `json:"text"`
			Score float64 `json:"score"`
		} `json:"results"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&exaResp); err != nil {
		return nil, err
	}

	res := &SearXNGSearchResult{
		Status:       "success",
		Query:        p.Query,
		TotalResults: len(exaResp.Results),
		Results:      make([]SearXNGSearchResultItem, 0, len(exaResp.Results)),
	}

	for _, item := range exaResp.Results {
		res.Results = append(res.Results, SearXNGSearchResultItem{
			Title:   item.Title,
			URL:     item.URL,
			Content: item.Text,
			Score:   item.Score,
			Engine:  "exa",
		})
	}
	return res, nil
}

// FirecrawlSearchBackend integrates with Firecrawl Search API.
type FirecrawlSearchBackend struct {
	client *http.Client
}

func (b *FirecrawlSearchBackend) Name() string { return "firecrawl" }

func (b *FirecrawlSearchBackend) Search(ctx context.Context, p SearchParams) (*SearXNGSearchResult, error) {
	endpoint := "https://api.firecrawl.dev/v1/search"
	if p.BaseURL != "" {
		endpoint = strings.TrimRight(p.BaseURL, "/") + "/v1/search"
	}

	maxResults := p.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}

	reqBody := map[string]any{
		"query": p.Query,
		"limit": maxResults,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: "firecrawl_http_error",
			Message:   fmt.Sprintf("Firecrawl returned HTTP %d", resp.StatusCode),
		}, nil
	}

	var fcResp struct {
		Data []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Markdown    string `json:"markdown"`
			Description string `json:"description"`
		} `json:"data"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&fcResp); err != nil {
		return nil, err
	}

	res := &SearXNGSearchResult{
		Status:       "success",
		Query:        p.Query,
		TotalResults: len(fcResp.Data),
		Results:      make([]SearXNGSearchResultItem, 0, len(fcResp.Data)),
	}

	for _, item := range fcResp.Data {
		content := item.Description
		if item.Markdown != "" {
			content = item.Markdown
		}
		res.Results = append(res.Results, SearXNGSearchResultItem{
			Title:   item.Title,
			URL:     item.URL,
			Content: content,
			Engine:  "firecrawl",
		})
	}
	return res, nil
}

// SerplySearchBackend integrates with Serply Google/Bing search proxy API.
type SerplySearchBackend struct {
	client *http.Client
}

func (b *SerplySearchBackend) Name() string { return "serply" }

func (b *SerplySearchBackend) Search(ctx context.Context, p SearchParams) (*SearXNGSearchResult, error) {
	endpoint := fmt.Sprintf("https://api.serply.io/v1/search/q=%s", url.QueryEscape(p.Query))
	if p.BaseURL != "" {
		endpoint = fmt.Sprintf("%s/v1/search/q=%s", strings.TrimRight(p.BaseURL, "/"), url.QueryEscape(p.Query))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if p.APIKey != "" {
		req.Header.Set("X-API-KEY", p.APIKey)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: "serply_http_error",
			Message:   fmt.Sprintf("Serply returned HTTP %d", resp.StatusCode),
		}, nil
	}

	var serplyResp struct {
		Results []struct {
			Title       string `json:"title"`
			Link        string `json:"link"`
			Description string `json:"description"`
		} `json:"results"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&serplyResp); err != nil {
		return nil, err
	}

	res := &SearXNGSearchResult{
		Status:       "success",
		Query:        p.Query,
		TotalResults: len(serplyResp.Results),
		Results:      make([]SearXNGSearchResultItem, 0, len(serplyResp.Results)),
	}

	limit := p.MaxResults
	if limit <= 0 {
		limit = 5
	}

	for i, item := range serplyResp.Results {
		if i >= limit {
			break
		}
		res.Results = append(res.Results, SearXNGSearchResultItem{
			Title:   item.Title,
			URL:     item.Link,
			Content: item.Description,
			Engine:  "serply",
		})
	}
	return res, nil
}

// HTTPJSONSearchBackend connects to an arbitrary implementor-provided HTTP JSON search endpoint.
type HTTPJSONSearchBackend struct {
	client *http.Client
}

func (b *HTTPJSONSearchBackend) Name() string { return "http_json" }

func (b *HTTPJSONSearchBackend) Search(ctx context.Context, p SearchParams) (*SearXNGSearchResult, error) {
	if p.BaseURL == "" {
		return nil, errors.New("http_json search requires base_url")
	}

	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}

	q := u.Query()
	q.Set("q", p.Query)
	if p.MaxResults > 0 {
		q.Set("limit", fmt.Sprintf("%d", p.MaxResults))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: "http_json_error",
			Message:   fmt.Sprintf("search backend returned HTTP %d", resp.StatusCode),
		}, nil
	}

	var jsonResp struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Snippet string  `json:"snippet"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&jsonResp); err != nil {
		return nil, err
	}

	res := &SearXNGSearchResult{
		Status:       "success",
		Query:        p.Query,
		TotalResults: len(jsonResp.Results),
		Results:      make([]SearXNGSearchResultItem, 0, len(jsonResp.Results)),
	}

	for _, item := range jsonResp.Results {
		content := item.Snippet
		if content == "" {
			content = item.Content
		}
		res.Results = append(res.Results, SearXNGSearchResultItem{
			Title:   item.Title,
			URL:     item.URL,
			Content: content,
			Score:   item.Score,
			Engine:  "http_json",
		})
	}
	return res, nil
}
