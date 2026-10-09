package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/core"
)

// Common error codes for SearXNG operations.
const (
	ErrCodeSearXNGDisabled         = "searxng_disabled"
	ErrCodeSearXNGUnreachable      = "searxng_unreachable"
	ErrCodeSearXNGTimeout          = "searxng_timeout"
	ErrCodeSearXNGHTTPError        = "searxng_http_error"
	ErrCodeSearXNGEmptyQuery       = "searxng_empty_query"
	ErrCodeSearXNGNoResults        = "searxng_no_results"
	ErrCodeSearXNGMalformedPayload = "searxng_malformed_response"
)

// SearXNGSearchResultItem represents an individual search result item from SearXNG.
type SearXNGSearchResultItem struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Engine  string  `json:"engine,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

// SearXNGSearchResult represents the normalized search output.
type SearXNGSearchResult struct {
	Status       string                    `json:"status"` // "success", "no_results", "unavailable", "disabled", "error"
	Query        string                    `json:"query"`
	TotalResults int                       `json:"total_results"`
	Results      []SearXNGSearchResultItem `json:"results"`
	Message      string                    `json:"message,omitempty"`
	ErrorCode    string                    `json:"error_code,omitempty"`
}

// SearXNGHealth represents the health inspection result of the SearXNG instance.
type SearXNGHealth struct {
	Configured bool   `json:"configured"`
	Enabled    bool   `json:"enabled"`
	URL        string `json:"url"`
	Status     string `json:"status"` // "healthy", "unreachable", "disabled", "error"
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

// SearXNGClient manages communication with an external SearXNG instance over HTTP/JSON.
type SearXNGClient struct {
	mu         sync.RWMutex
	baseURL    string
	enabled    bool
	timeout    time.Duration
	httpClient *http.Client
}

// NewSearXNGClient creates a client to communicate with a SearXNG instance.
func NewSearXNGClient(baseURL string, enabled bool, timeout time.Duration, customClient *http.Client) *SearXNGClient {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	cleanURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")

	client := customClient
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          20,
				IdleConnTimeout:       30 * time.Second,
				ResponseHeaderTimeout: timeout,
			},
		}
	}

	return &SearXNGClient{
		baseURL:    cleanURL,
		enabled:    enabled,
		timeout:    timeout,
		httpClient: client,
	}
}

// URL returns the configured base URL.
func (c *SearXNGClient) URL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// IsEnabled returns whether SearXNG is globally enabled.
func (c *SearXNGClient) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled && c.baseURL != ""
}

// Config returns the current configuration parameters.
func (c *SearXNGClient) Config() (string, bool, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.enabled, c.timeout
}

// UpdateConfig updates the SearXNG URL, enabled status, and timeout at runtime.
func (c *SearXNGClient) UpdateConfig(baseURL string, enabled bool, timeout time.Duration) {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	cleanURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")

	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = cleanURL
	c.enabled = enabled
	c.timeout = timeout
	c.httpClient = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          20,
			IdleConnTimeout:       30 * time.Second,
			ResponseHeaderTimeout: timeout,
		},
	}
}

// CheckHealth queries the SearXNG instance to verify its readiness and measuring round-trip latency.
func (c *SearXNGClient) CheckHealth(ctx context.Context) *SearXNGHealth {
	c.mu.RLock()
	baseURL := c.baseURL
	enabled := c.enabled
	client := c.httpClient
	c.mu.RUnlock()

	health := &SearXNGHealth{
		Configured: baseURL != "",
		Enabled:    enabled,
		URL:        baseURL,
		Status:     "disabled",
	}

	if !enabled || baseURL == "" {
		health.Status = "disabled"
		health.Error = "SearXNG search service is disabled or unconfigured"
		return health
	}

	endpoint := baseURL + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		health.Status = "error"
		health.Error = fmt.Sprintf("build health request: %v", err)
		return health
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	health.LatencyMS = latency

	if err != nil {
		// Try fallback ping to /search?q=ping&format=json in case /healthz is not exposed
		fallbackEndpoint := baseURL + "/search?q=ping&format=json"
		fallbackReq, fbErr := http.NewRequestWithContext(ctx, http.MethodGet, fallbackEndpoint, nil)
		if fbErr == nil {
			startFb := time.Now()
			if fbResp, fbDoErr := client.Do(fallbackReq); fbDoErr == nil {
				_ = fbResp.Body.Close()
				if fbResp.StatusCode < 500 {
					health.Status = "healthy"
					health.LatencyMS = time.Since(startFb).Milliseconds()
					return health
				}
			}
		}

		health.Status = "unreachable"
		health.Error = parseNetworkError(err)
		return health
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		health.Status = "healthy"
	} else {
		health.Status = "unreachable"
		health.Error = fmt.Sprintf("SearXNG returned HTTP %d", resp.StatusCode)
	}

	return health
}

// Search performs a query against SearXNG and returns parsed, bounded search results.
func (c *SearXNGClient) Search(
	ctx context.Context,
	query string,
	categories []string,
	engines []string,
	timeRange string,
	safeSearch int,
	maxResults int,
) (*SearXNGSearchResult, error) {
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: ErrCodeSearXNGEmptyQuery,
			Message:   "Search query cannot be empty.",
		}, nil
	}

	c.mu.RLock()
	baseURL := c.baseURL
	enabled := c.enabled
	timeout := c.timeout
	client := c.httpClient
	c.mu.RUnlock()

	if !enabled || baseURL == "" {
		return &SearXNGSearchResult{
			Status:    "disabled",
			ErrorCode: ErrCodeSearXNGDisabled,
			Message:   "Web search service is currently not configured or disabled on this Beavend instance.",
		}, nil
	}

	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}

	// Prepare URL parameters
	params := url.Values{}
	params.Set("q", trimmedQuery)
	params.Set("format", "json")

	if len(categories) > 0 {
		params.Set("categories", strings.Join(categories, ","))
	}
	if len(engines) > 0 {
		params.Set("engines", strings.Join(engines, ","))
	}
	if timeRange != "" {
		params.Set("time_range", timeRange)
	}
	if safeSearch >= 0 && safeSearch <= 2 {
		params.Set("safesearch", fmt.Sprintf("%d", safeSearch))
	}

	searchURL := fmt.Sprintf("%s/search?%s", baseURL, params.Encode())

	searchCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		searchCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(searchCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: ErrCodeSearXNGHTTPError,
			Message:   fmt.Sprintf("Failed to construct search request: %v", err),
		}, nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Beavend-Search-Agent/1.0")

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
			return &SearXNGSearchResult{
				Status:    "unavailable",
				ErrorCode: ErrCodeSearXNGTimeout,
				Message:   fmt.Sprintf("Search engine request timed out after %s.", timeout),
			}, nil
		}

		return &SearXNGSearchResult{
			Status:    "unavailable",
			ErrorCode: ErrCodeSearXNGUnreachable,
			Message:   fmt.Sprintf("Search engine is currently unreachable: %s.", parseNetworkError(err)),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodySample, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: ErrCodeSearXNGHTTPError,
			Message:   fmt.Sprintf("Search engine returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodySample))),
		}, nil
	}

	// Limit reader to 2MB to protect memory
	limitedReader := io.LimitReader(resp.Body, 2<<20)

	var raw struct {
		Query           string `json:"query"`
		NumberOfResults int    `json:"number_of_results"`
		Results         []struct {
			URL     string  `json:"url"`
			Title   string  `json:"title"`
			Content string  `json:"content"`
			Engine  string  `json:"engine"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}

	if err := json.NewDecoder(limitedReader).Decode(&raw); err != nil {
		return &SearXNGSearchResult{
			Status:    "error",
			ErrorCode: ErrCodeSearXNGMalformedPayload,
			Message:   fmt.Sprintf("Failed to parse search engine JSON response: %v", err),
		}, nil
	}

	if len(raw.Results) == 0 {
		return &SearXNGSearchResult{
			Status:       "no_results",
			ErrorCode:    ErrCodeSearXNGNoResults,
			Query:        trimmedQuery,
			TotalResults: 0,
			Results:      []SearXNGSearchResultItem{},
			Message:      fmt.Sprintf("No search results found for query %q.", trimmedQuery),
		}, nil
	}

	var items []SearXNGSearchResultItem
	for i, r := range raw.Results {
		if i >= maxResults {
			break
		}
		// Truncate snippet content to 600 characters max to prevent token bloat
		content := strings.TrimSpace(r.Content)
		if len(content) > 600 {
			content = content[:600] + "..."
		}
		items = append(items, SearXNGSearchResultItem{
			Title:   strings.TrimSpace(r.Title),
			URL:     strings.TrimSpace(r.URL),
			Content: content,
			Engine:  r.Engine,
			Score:   r.Score,
		})
	}

	return &SearXNGSearchResult{
		Status:       "success",
		Query:        trimmedQuery,
		TotalResults: raw.NumberOfResults,
		Results:      items,
	}, nil
}

func parseNetworkError(err error) string {
	if err == nil {
		return "unknown network error"
	}
	var netOpErr *net.OpError
	if errors.As(err, &netOpErr) {
		return fmt.Sprintf("network error on %s: %s", netOpErr.Op, netOpErr.Err.Error())
	}
	return err.Error()
}

// SearXNGExecutor implements core.Executor for AI App web search.
type SearXNGExecutor struct {
	client    *SearXNGClient
	appConfig core.SearchConfig
}

// NewSearXNGExecutor creates a new SearXNGExecutor.
func NewSearXNGExecutor(client *SearXNGClient, cfg core.SearchConfig) *SearXNGExecutor {
	return &SearXNGExecutor{
		client:    client,
		appConfig: cfg,
	}
}

// BuiltinSearchToolDefinition generates the standard core.ToolDefinition for web_search.
func BuiltinSearchToolDefinition(cfg core.SearchConfig) core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "web_search",
		Name:        "web_search",
		Description: "Search the web for up-to-date facts, information, documentation, news, and technical references.",
		SideEffect:  "read",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Keywords or search phrase to query the web for.",
				},
				"category": map[string]any{
					"type":        "string",
					"description": "Optional search category: general, news, science, it.",
				},
			},
			"required": []any{"query"},
		},
		Executor: core.ExecutorConfig{
			Type: "searxng",
		},
	}
}

// Execute executes the web search tool call and formats the result for the LLM turn.
func (e *SearXNGExecutor) Execute(ctx context.Context, call core.ToolCall, _ core.Identity) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","message":"request cancelled: %v"}`, err),
			IsError: true,
		}, nil
	}

	var args struct {
		Query    string `json:"query"`
		Category string `json:"category"`
	}

	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"error","error_code":"invalid_arguments","message":"Failed to parse arguments JSON: %v"}`, err),
			IsError: true,
		}, nil
	}

	if strings.TrimSpace(args.Query) == "" {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: `{"status":"error","error_code":"searxng_empty_query","message":"The search query cannot be empty. Please provide search terms."}`,
			IsError: true,
		}, nil
	}

	if e.client == nil || !e.client.IsEnabled() {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: `{"status":"disabled","message":"Web search capability is not configured or disabled on this Beavend instance. Please continue with your existing knowledge."}`,
			IsError: false,
		}, nil
	}

	// Determine categories
	categories := e.appConfig.Categories
	if args.Category != "" {
		categories = []string{args.Category}
	}

	maxResults := e.appConfig.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}

	res, err := e.client.Search(
		ctx,
		args.Query,
		categories,
		e.appConfig.Engines,
		e.appConfig.TimeRange,
		e.appConfig.SafeSearch,
		maxResults,
	)
	if err != nil {
		return core.ToolResult{
			CallID:  call.ID,
			ToolID:  call.Name,
			Content: fmt.Sprintf(`{"status":"unavailable","message":"Search engine execution error: %v. Please continue with your internal knowledge."}`, err),
			IsError: false,
		}, nil
	}

	payload, _ := json.Marshal(res)
	return core.ToolResult{
		CallID:   call.ID,
		ToolID:   call.Name,
		Content:  string(payload),
		IsError:  false,
		Metadata: map[string]any{"total_results": res.TotalResults, "status": res.Status},
	}, nil
}
