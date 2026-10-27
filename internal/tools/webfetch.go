package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/beavend/beavend/internal/core"
)

const (
	// MaxWebFetchBodyBytes is 1MB limit to protect against decompression / memory exhaustion bombs.
	MaxWebFetchBodyBytes = 1 << 20 // 1 MB
	// DefaultWebFetchTimeout is 30 seconds per specification.
	DefaultWebFetchTimeout = 30 * time.Second
)

var (
	stripTagsRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`),
		regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`),
		regexp.MustCompile(`(?is)<svg[^>]*>.*?</svg>`),
		regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`),
		regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`),
		regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`),
	}
	tagRegex      = regexp.MustCompile(`(?s)<[^>]+>`)
	h1Regex       = regexp.MustCompile(`(?i)<h1[^>]*>(.*?)</h1>`)
	h2Regex       = regexp.MustCompile(`(?i)<h2[^>]*>(.*?)</h2>`)
	h3H6Regex     = regexp.MustCompile(`(?i)<h[3-6][^>]*>(.*?)</h[3-6]>`)
	bRegex        = regexp.MustCompile(`(?i)<(?:b|strong)[^>]*>(.*?)</(?:b|strong)>`)
	pBreakRegex   = regexp.MustCompile(`(?i)<(?:p|br|div|tr)[^>]*>`)
	liRegex       = regexp.MustCompile(`(?i)<li[^>]*>(.*?)</li>`)
	aHrefRegex    = regexp.MustCompile(`(?i)<a\s+[^>]*href=["']([^"']*)["'][^>]*>(.*?)</a>`)
	multiSpace    = regexp.MustCompile(`[ \t]+`)
	multiNewlines = regexp.MustCompile(`\n{3,}`)
)

// WebFetchTool executes SSRF-safe HTTP fetching with robots.txt enforcement and HTML to markdown conversion.
type WebFetchTool struct {
	client    *http.Client
	sanitizer *Sanitizer
}

// WebFetchOptions holds configuration for a web fetch invocation.
type WebFetchOptions struct {
	URL         string
	HonorRobots bool
	Nonce       string
	CallID      string
}

// NewWebFetchClient creates an SSRF-safe HTTP client restricted to ports 80 and 443 with max 5 redirects.
func NewWebFetchClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultWebFetchTimeout
	}
	transport := NewSafeTransport(SafeDialerConfig{
		AllowedPorts: []uint16{80, 443},
		DialTimeout:  10 * time.Second,
		KeepAlive:    30 * time.Second,
	})
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("web_fetch: stopped after 5 redirects")
			}
			lastReq := via[len(via)-1]
			if lastReq.URL.Scheme == "https" && req.URL.Scheme == "http" {
				return errors.New("web_fetch: redirect downgrade from https to http forbidden")
			}
			return nil
		},
	}
}

// NewWebFetchTool creates a new WebFetchTool instance.
func NewWebFetchTool(client *http.Client, sanitizer *Sanitizer) *WebFetchTool {
	if client == nil {
		client = NewWebFetchClient(DefaultWebFetchTimeout)
	}
	if sanitizer == nil {
		sanitizer = NewSanitizer()
	}
	return &WebFetchTool{
		client:    client,
		sanitizer: sanitizer,
	}
}

// Execute fetches the specified URL, verifies content types, cleans HTML to markdown, and wraps with Sanitizer.
func (t *WebFetchTool) Execute(ctx context.Context, opts WebFetchOptions) (core.ToolResult, error) {
	result := core.ToolResult{
		ToolID: "web_fetch",
		CallID: opts.CallID,
	}

	targetURL := strings.TrimSpace(opts.URL)
	if targetURL == "" {
		result.IsError = true
		result.Content = "web_fetch: empty URL provided"
		return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		result.IsError = true
		result.Content = fmt.Sprintf("web_fetch: invalid URL or unsupported scheme %q", targetURL)
		return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
	}

	// Honor robots.txt when requested
	if opts.HonorRobots {
		allowed, err := t.checkRobots(ctx, parsedURL)
		if err == nil && !allowed {
			result.IsError = true
			result.Content = fmt.Sprintf("web_fetch: access to %s disallowed by robots.txt", targetURL)
			return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		result.IsError = true
		result.Content = fmt.Sprintf("web_fetch: failed to create request: %v", err)
		return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
	}

	req.Header.Set("User-Agent", "BeavendBot/1.0 (+https://beavend.dev/bot)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		result.IsError = true
		result.Content = fmt.Sprintf("web_fetch error: %v", err)
		return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		result.IsError = true
		result.Content = fmt.Sprintf("web_fetch: HTTP %d %s", resp.StatusCode, resp.Status)
		return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
	}

	// Read bounded body up to 1MB using LimitReader
	limitedReader := io.LimitReader(resp.Body, MaxWebFetchBodyBytes)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		result.IsError = true
		result.Content = fmt.Sprintf("web_fetch read error: %v", err)
		return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
	}

	// Validate MIME type
	sniffLen := len(bodyBytes)
	if sniffLen > 512 {
		sniffLen = 512
	}
	contentType := http.DetectContentType(bodyBytes[:sniffLen])
	if !isAllowedContentType(contentType) {
		result.IsError = true
		result.Content = fmt.Sprintf("web_fetch: disallowed MIME content type %q", contentType)
		return t.sanitizer.Sanitize(result, core.ResultConfig{}, WithNonce(opts.Nonce)), nil
	}

	// Convert HTML to Markdown or clean text
	converted := HTMLToMarkdown(string(bodyBytes))
	result.Content = converted

	return t.sanitizer.Sanitize(result, core.ResultConfig{MaxBytes: HardMaxBytes}, WithNonce(opts.Nonce)), nil
}

func (t *WebFetchTool) checkRobots(ctx context.Context, targetURL *url.URL) (bool, error) {
	robotsURL := fmt.Sprintf("%s://%s/robots.txt", targetURL.Scheme, targetURL.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return true, nil
	}
	req.Header.Set("User-Agent", "BeavendBot/1.0")

	resp, err := t.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		// If robots.txt is missing or unreachable, allow
		return true, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return true, nil
	}

	return isPathAllowedByRobots(string(body), targetURL.Path), nil
}

func isPathAllowedByRobots(robotsTxt, reqPath string) bool {
	lines := strings.Split(robotsTxt, "\n")
	activeAgent := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		field := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		if field == "user-agent" {
			agent := strings.ToLower(val)
			activeAgent = (agent == "*" || strings.Contains(agent, "beavend"))
		} else if activeAgent && field == "disallow" {
			if val != "" && strings.HasPrefix(reqPath, val) {
				return false
			}
		}
	}
	return true
}

func isAllowedContentType(ct string) bool {
	ctLower := strings.ToLower(ct)
	allowed := []string{
		"text/html", "text/plain", "application/json", "application/xml",
		"text/xml", "text/markdown", "application/xhtml+xml",
	}
	for _, a := range allowed {
		if strings.HasPrefix(ctLower, a) {
			return true
		}
	}
	return false
}

// HTMLToMarkdown converts raw HTML into readable Markdown text.
func HTMLToMarkdown(rawHTML string) string {
	// 1. Strip script, style, nav, header, footer tags completely
	s := rawHTML
	for _, re := range stripTagsRegexes {
		s = re.ReplaceAllString(s, "")
	}

	// 2. Headings
	s = h1Regex.ReplaceAllString(s, "\n\n# $1\n\n")
	s = h2Regex.ReplaceAllString(s, "\n\n## $1\n\n")
	s = h3H6Regex.ReplaceAllString(s, "\n\n### $1\n\n")

	// 3. Bold / Strong
	s = bRegex.ReplaceAllString(s, "**$1**")

	// 4. Links
	s = aHrefRegex.ReplaceAllString(s, "[$2]($1)")

	// 5. List items
	s = liRegex.ReplaceAllString(s, "\n- $1")

	// 6. Paragraphs & breaks
	s = pBreakRegex.ReplaceAllString(s, "\n\n")

	// 7. Strip all other tags
	s = tagRegex.ReplaceAllString(s, " ")

	// 8. Unescape entities
	s = html.UnescapeString(s)

	// 9. Collapse whitespace and multiple newlines
	lines := strings.Split(s, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(multiSpace.ReplaceAllString(line, " "))
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		} else if len(cleaned) > 0 && cleaned[len(cleaned)-1] != "" {
			cleaned = append(cleaned, "")
		}
	}

	result := strings.TrimSpace(strings.Join(cleaned, "\n"))
	return multiNewlines.ReplaceAllString(result, "\n\n")
}
