package tools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var (
	// ErrBlockedHost is returned when a requested host is explicitly denied.
	ErrBlockedHost = errors.New("ssrf: blocked host")
	// ErrBlockedIP is returned when a resolved IP falls within a denied range.
	ErrBlockedIP = errors.New("ssrf: blocked destination IP")
	// ErrNoSafeIPs is returned when no IP resolved for a host was deemed safe.
	ErrNoSafeIPs = errors.New("ssrf: no safe IP addresses found")
)

var blockedHostnames = map[string]struct{}{
	"metadata.google.internal": {},
	"metadata.internal":        {},
	"instance-data":            {},
	"localhost":                {},
}

var defaultDeniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),     // IPv4 Loopback
	netip.MustParsePrefix("10.0.0.0/8"),      // RFC 1918 Private
	netip.MustParsePrefix("172.16.0.0/12"),   // RFC 1918 Private
	netip.MustParsePrefix("192.168.0.0/16"),  // RFC 1918 Private
	netip.MustParsePrefix("169.254.0.0/16"),  // RFC 3927 Link-Local / Cloud Metadata
	netip.MustParsePrefix("0.0.0.0/8"),       // Current network
	netip.MustParsePrefix("100.64.0.0/10"),   // Shared address space (CGNAT)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF Protocol Assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("224.0.0.0/4"),     // Multicast
	netip.MustParsePrefix("240.0.0.0/4"),     // Reserved
	netip.MustParsePrefix("::1/128"),         // IPv6 Loopback
	netip.MustParsePrefix("fc00::/7"),        // IPv6 Unique Local Address
	netip.MustParsePrefix("fe80::/10"),       // IPv6 Link-Local
}

// SafeDialerConfig configures the SSRF-safe dialer.
type SafeDialerConfig struct {
	DialTimeout     time.Duration
	KeepAlive       time.Duration
	AllowPrivate    bool           // If true, allows private/loopback IPs (e.g. for testing)
	DeniedPrefixes  []netip.Prefix // Overrides or adds to default denied prefixes
	AllowedPrefixes []netip.Prefix // If specified, only IPs matching these prefixes are allowed
	Resolver        *net.Resolver
}

// SafeDialer provides SSRF-safe dialing with DNS-rebinding mitigation.
type SafeDialer struct {
	cfg SafeDialerConfig
}

// NewSafeDialer creates a new SafeDialer with the specified configuration.
func NewSafeDialer(cfg SafeDialerConfig) *SafeDialer {
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 30 * time.Second
	}
	if cfg.Resolver == nil {
		cfg.Resolver = net.DefaultResolver
	}
	if len(cfg.DeniedPrefixes) == 0 {
		cfg.DeniedPrefixes = defaultDeniedPrefixes
	}
	return &SafeDialer{cfg: cfg}
}

// IsSafeIP checks whether a given IP address is permitted.
func (d *SafeDialer) IsSafeIP(ip netip.Addr) bool {
	// Unmap IPv4-mapped IPv6 (e.g., ::ffff:127.0.0.1 -> 127.0.0.1)
	unmapped := ip.Unmap()

	if d.cfg.AllowPrivate {
		return true
	}

	// Check built-in properties
	if unmapped.IsLoopback() || unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() || unmapped.IsPrivate() || unmapped.IsMulticast() || unmapped.IsUnspecified() {
		return false
	}

	// Check against explicitly denied prefixes
	for _, prefix := range d.cfg.DeniedPrefixes {
		if prefix.Contains(unmapped) {
			return false
		}
	}

	// If explicit allowlist is configured, verify membership
	if len(d.cfg.AllowedPrefixes) > 0 {
		allowed := false
		for _, prefix := range d.cfg.AllowedPrefixes {
			if prefix.Contains(unmapped) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	return true
}

// DialContext resolves the host, validates all IP addresses against SSRF rules,
// and dials the verified IP to prevent DNS rebinding attacks.
func (d *SafeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("ssrf: invalid target address %q: %w", addr, err)
	}

	hostLower := strings.ToLower(host)
	if !d.cfg.AllowPrivate {
		if _, blocked := blockedHostnames[hostLower]; blocked {
			return nil, fmt.Errorf("%w: %s", ErrBlockedHost, host)
		}
	}

	var ips []netip.Addr

	// If host is an IP literal
	if parsedIP, err := netip.ParseAddr(host); err == nil {
		ips = append(ips, parsedIP)
	} else {
		// Resolve domain name
		netIPs, err := d.cfg.Resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("ssrf: dns lookup failed for %s: %w", host, err)
		}
		ips = netIPs
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("%w for %s", ErrNoSafeIPs, host)
	}

	// Verify all resolved IPs are safe
	var safeIPs []netip.Addr
	for _, ip := range ips {
		if !d.IsSafeIP(ip) {
			if !d.cfg.AllowPrivate {
				return nil, fmt.Errorf("%w: host %s resolves to forbidden IP %s", ErrBlockedIP, host, ip.String())
			}
		}
		safeIPs = append(safeIPs, ip)
	}

	if len(safeIPs) == 0 {
		return nil, fmt.Errorf("%w for %s", ErrNoSafeIPs, host)
	}

	// Connect to the first verified safe IP (mitigating DNS rebinding)
	verifiedTarget := net.JoinHostPort(safeIPs[0].String(), port)
	dialer := &net.Dialer{
		Timeout:   d.cfg.DialTimeout,
		KeepAlive: d.cfg.KeepAlive,
	}

	return dialer.DialContext(ctx, network, verifiedTarget)
}

// NewSafeTransport creates an http.Transport configured with the SafeDialer.
func NewSafeTransport(cfg SafeDialerConfig) *http.Transport {
	dialer := NewSafeDialer(cfg)
	return &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewSafeHTTPClient creates an *http.Client configured with SSRF protection.
func NewSafeHTTPClient(cfg SafeDialerConfig, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{
		Transport: NewSafeTransport(cfg),
		Timeout:   timeout,
	}
}
