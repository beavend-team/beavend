package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLToMarkdown(t *testing.T) {
	htmlInput := `
<!DOCTYPE html>
<html>
<head><title>Test Page</title><style>.banner{color:red;}</style></head>
<body>
<nav><a href="/home">Home</a></nav>
<h1>Main Title</h1>
<p>This is a paragraph with <a href="https://example.com/info">a link</a> and <b>bold text</b>.</p>
<h2>Sub Heading</h2>
<ul>
  <li>Item 1</li>
  <li>Item 2</li>
</ul>
<script>console.log("malicious code")</script>
<footer>Copyright 2026</footer>
</body>
</html>`

	md := HTMLToMarkdown(htmlInput)

	assert.NotContains(t, md, "malicious code")
	assert.NotContains(t, md, ".banner{color:red;}")
	assert.NotContains(t, md, "Copyright 2026")
	assert.Contains(t, md, "# Main Title")
	assert.Contains(t, md, "## Sub Heading")
	assert.Contains(t, md, "[a link](https://example.com/info)")
	assert.Contains(t, md, "- Item 1")
	assert.Contains(t, md, "- Item 2")
}

func TestWebFetchTool_ExecutionAndLimits(t *testing.T) {
	// Create test server serving various endpoints
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nDisallow: /blocked\n")
		case "/blocked":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body>Secret Area</body></html>")
		case "/huge":
			w.Header().Set("Content-Type", "text/plain")
			// Stream 2MB of 'A' characters
			chunk := strings.Repeat("A", 64*1024)
			for i := 0; i < 32; i++ {
				_, _ = w.Write([]byte(chunk))
			}
		case "/binary":
			w.Header().Set("Content-Type", "application/zip")
			// Fake zip magic bytes
			_, _ = w.Write([]byte("PK\x03\x04binary-data-stream"))
		case "/article":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h1>Article Header</h1><p>Article body content.</p></body></html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Parse server port
	tsURL, err := url.Parse(ts.URL)
	require.NoError(t, err)
	_, portStr, err := net.SplitHostPort(tsURL.Host)
	require.NoError(t, err)

	var serverPort uint16
	fmt.Sscanf(portStr, "%d", &serverPort)

	// Create web fetch tool configured to permit the test server's port and loopback
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: NewSafeTransport(SafeDialerConfig{
			AllowPrivate: true,
			AllowedPorts: []uint16{serverPort, 80, 443},
		}),
	}
	tool := NewWebFetchTool(client, NewSanitizer())
	ctx := context.Background()

	t.Run("successful fetch with nonce wrapping", func(t *testing.T) {
		res, err := tool.Execute(ctx, WebFetchOptions{
			URL:    ts.URL + "/article",
			Nonce:  "nonce_wf_1",
			CallID: "call_wf_1",
		})
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Contains(t, res.Content, `<tool_result nonce="nonce_wf_1" tool_id="web_fetch" call_id="call_wf_1" status="success">`)
		assert.Contains(t, res.Content, "# Article Header")
		assert.Contains(t, res.Content, "Article body content.")
	})

	t.Run("robots.txt blocks disallowed path", func(t *testing.T) {
		res, err := tool.Execute(ctx, WebFetchOptions{
			URL:         ts.URL + "/blocked",
			HonorRobots: true,
			Nonce:       "nonce_wf_2",
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Content, "disallowed by robots.txt")
	})

	t.Run("disallowed binary MIME type rejected", func(t *testing.T) {
		res, err := tool.Execute(ctx, WebFetchOptions{
			URL:   ts.URL + "/binary",
			Nonce: "nonce_wf_3",
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Content, "disallowed MIME content type")
	})

	t.Run("1MB body limit prevents memory explosion", func(t *testing.T) {
		res, err := tool.Execute(ctx, WebFetchOptions{
			URL:   ts.URL + "/huge",
			Nonce: "nonce_wf_4",
		})
		require.NoError(t, err)
		// Body must not exceed 1MB + wrapping overhead
		assert.LessOrEqual(t, len(res.Content), MaxWebFetchBodyBytes+4096)
	})

	t.Run("port filter rejects non-permitted port", func(t *testing.T) {
		restrictedClient := &http.Client{
			Transport: NewSafeTransport(SafeDialerConfig{
				AllowPrivate: true,
				AllowedPorts: []uint16{80, 443}, // ts port is not 80 or 443
			}),
		}
		restrictedTool := NewWebFetchTool(restrictedClient, NewSanitizer())

		res, err := restrictedTool.Execute(ctx, WebFetchOptions{
			URL:   ts.URL + "/article",
			Nonce: "nonce_wf_5",
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, res.Content, "is not in allowed ports")
	})
}
