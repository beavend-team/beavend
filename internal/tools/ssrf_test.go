package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestSafeDialer_IsSafeIP(t *testing.T) {
	dialer := NewSafeDialer(SafeDialerConfig{})

	tests := []struct {
		name string
		ip   string
		safe bool
	}{
		{"IPv4 Loopback", "127.0.0.1", false},
		{"IPv4 Loopback high", "127.255.255.254", false},
		{"IPv6 Loopback", "::1", false},
		{"IPv4-mapped IPv6 loopback", "::ffff:127.0.0.1", false},
		{"IPv4-mapped IPv6 private", "::ffff:10.0.0.1", false},
		{"RFC1918 10/8", "10.1.2.3", false},
		{"RFC1918 172.16/12", "172.20.1.1", false},
		{"RFC1918 192.168/16", "192.168.0.1", false},
		{"AWS Metadata", "169.254.169.254", false},
		{"Link local IPv4", "169.254.1.2", false},
		{"Link local IPv6", "fe80::1", false},
		{"IPv6 Unique Local", "fc00::1", false},
		{"IPv6 Unique Local fd00", "fd12:3456:789a::1", false},
		{"Multicast IPv4", "224.0.0.251", false},
		{"Zero network", "0.0.0.0", false},
		{"Public Cloudflare DNS", "1.1.1.1", true},
		{"Public Google DNS", "8.8.8.8", true},
		{"Public Cloudflare IPv6", "2606:4700:4700::1111", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := netip.ParseAddr(tt.ip)
			if err != nil {
				t.Fatalf("failed to parse test IP %q: %v", tt.ip, err)
			}
			got := dialer.IsSafeIP(addr)
			if got != tt.safe {
				t.Errorf("IsSafeIP(%q) = %v; want %v", tt.ip, got, tt.safe)
			}
		})
	}
}

func TestSafeDialer_BlockedHostnames(t *testing.T) {
	dialer := NewSafeDialer(SafeDialerConfig{})

	blocked := []string{
		"localhost:80",
		"metadata.google.internal:80",
		"metadata.internal:443",
		"instance-data:80",
	}

	ctx := context.Background()
	for _, target := range blocked {
		t.Run(target, func(t *testing.T) {
			_, err := dialer.DialContext(ctx, "tcp", target)
			if err == nil {
				t.Fatalf("expected error dialing %s, got nil", target)
			}
			if !errors.Is(err, ErrBlockedHost) {
				t.Errorf("expected ErrBlockedHost, got: %v", err)
			}
		})
	}
}

func TestSafeDialer_BlockedIPDial(t *testing.T) {
	dialer := NewSafeDialer(SafeDialerConfig{})
	ctx := context.Background()

	targets := []string{
		"127.0.0.1:8080",
		"10.0.0.5:80",
		"169.254.169.254:80",
		"[::1]:8080",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			_, err := dialer.DialContext(ctx, "tcp", target)
			if err == nil {
				t.Fatalf("expected error dialing %s, got nil", target)
			}
			if !errors.Is(err, ErrBlockedIP) {
				t.Errorf("expected ErrBlockedIP, got: %v", err)
			}
		})
	}
}

func TestSafeDialer_AllowPrivateForLocalTesting(t *testing.T) {
	// Spin up a local test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	// Safe dialer with AllowPrivate=true should connect successfully
	client := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: true}, 5*time.Second)

	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error with AllowPrivate=true: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Safe dialer with default config (AllowPrivate=false) MUST reject local test server
	blockedClient := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: false}, 5*time.Second)
	_, err = blockedClient.Get(ts.URL)
	if err == nil {
		t.Fatalf("expected connection to local test server to be blocked, but succeeded")
	}
}

func TestSafeHTTPClient_CheckRedirect(t *testing.T) {
	client := NewSafeHTTPClient(SafeDialerConfig{AllowPrivate: true}, 5*time.Second)

	// 1. Disallow downgrade from https to http
	httpsReq, _ := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	httpReq, _ := http.NewRequest(http.MethodGet, "http://example.com/insecure", nil)
	err := client.CheckRedirect(httpReq, []*http.Request{httpsReq})
	if err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("expected error on https to http downgrade, got %v", err)
	}

	// 2. Stopped after 10 redirects
	via := make([]*http.Request, 10)
	for i := range via {
		via[i], _ = http.NewRequest(http.MethodGet, "https://example.com/step", nil)
	}
	nextReq, _ := http.NewRequest(http.MethodGet, "https://example.com/step11", nil)
	err = client.CheckRedirect(nextReq, via)
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected 10 redirects error, got %v", err)
	}

	// 3. Sensitive headers stripped on cross-origin redirect
	origReq, _ := http.NewRequest(http.MethodGet, "https://api.internal.com/data", nil)
	redirectReq, _ := http.NewRequest(http.MethodGet, "https://external.thirdparty.com/callback", nil)
	redirectReq.Header.Set("Authorization", "Bearer secret-token")
	redirectReq.Header.Set("X-Beavend-Signature", "abc123sig")
	redirectReq.Header.Set("X-User-Subject", "usr_alice")
	redirectReq.Header.Set("Content-Type", "application/json")

	err = client.CheckRedirect(redirectReq, []*http.Request{origReq})
	if err != nil {
		t.Fatalf("unexpected redirect error: %v", err)
	}

	if redirectReq.Header.Get("Authorization") != "" {
		t.Errorf("Authorization header was not stripped on cross-origin redirect")
	}
	if redirectReq.Header.Get("X-Beavend-Signature") != "" {
		t.Errorf("X-Beavend-Signature was not stripped on cross-origin redirect")
	}
	if redirectReq.Header.Get("X-User-Subject") != "" {
		t.Errorf("X-User-Subject was not stripped on cross-origin redirect")
	}
	if redirectReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type should have been preserved, got %q", redirectReq.Header.Get("Content-Type"))
	}
}
