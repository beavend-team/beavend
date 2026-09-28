# T031: SSRF Guard & DNS-Rebinding-Safe Dialer

## Status
Completed

## Context & Objectives
To prevent Server-Side Request Forgery (SSRF) against private VPC infrastructure, loopback interfaces, link-local addresses, and cloud metadata services (e.g. AWS `169.254.169.254`, GCP `metadata.google.internal`), HTTP tool execution must run through a hardened egress dialer.

The dialer must:
1. Deny loopback (`127.0.0.0/8`, `::1`).
2. Deny RFC 1918 private IPv4 ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`).
3. Deny RFC 3927 link-local IPv4 ranges (`169.254.0.0/16`) and IPv6 link-local (`fe80::/10`).
4. Deny cloud metadata IP `169.254.169.254` and known metadata hostnames (`metadata.google.internal`, `instance-data`).
5. Mitigate DNS rebinding: resolve IP addresses, check that all resolved IPs are safe, and dial the verified IP directly while preserving the original Host header and TLS SNI server name.
6. Provide an allowlist/denylist configuration option for test environments or specific allowed egress CIDRs.

## Deliverables
* `internal/tools/ssrf.go`: `SafeDialer` implementing a custom `net.Dialer` / `http.Transport` dial context function with IP range checks and DNS rebinding prevention.
* `internal/tools/ssrf_test.go`: Comprehensive unit tests covering private IPs, loopback, cloud metadata, public IPs, and DNS rebinding simulation.
