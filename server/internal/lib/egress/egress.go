// Package egress builds HTTP transports that pin every connection to the
// addresses resolved once at construction, so DNS rebinding cannot redirect a
// later request. It is shared by the embedding client and the harness HTTP
// tool; the two differ only in how they interpret a policy rejection.
package egress

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Policy is the egress guard. AllowedHosts holds exact lowercase hostnames
// with no ports or wildcards. Unlisted hosts are rejected. Private and
// loopback addresses are rejected unless AllowPrivate is set; every
// non-private target requires HTTPS and a globally routable address.
type Policy struct {
	AllowedHosts []string
	AllowPrivate bool
	Timeout      time.Duration
}

// Error is a sanitized policy or transport rejection. Code is a stable,
// machine-readable reason; Retryable marks transient dial failures.
type Error struct {
	Code      string
	Retryable bool
}

func (e *Error) Error() string { return "egress: " + e.Code }

func bad(code string) *Error { return &Error{Code: code} }

// IsRetryable reports whether err is a transient egress failure.
func IsRetryable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retryable
}

// Code returns the egress error code of err, or "" for any other error.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// New resolves target once, validates the answer against policy, and returns
// the pinned endpoint (scheme://host:port/path) plus a dedicated proxy-free
// transport whose dialer accepts only the pinned address. Redirects are never
// followed. Callers must use the returned endpoint, not the original target.
func New(policy Policy, target string) (string, *http.Transport, error) {
	badRequest := bad("invalid_endpoint")
	u, err := url.Parse(target)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.RawPath != "" || u.Opaque != "" || strings.Contains(target, "#") ||
		(u.Scheme != "https" && u.Scheme != "http") {
		return "", nil, badRequest
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, entry := range policy.AllowedHosts {
		if strings.EqualFold(entry, host) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", nil, badRequest
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return "", nil, badRequest
	}
	ctx, cancel := context.WithTimeout(context.Background(), policy.Timeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	for i := range ips {
		ips[i] = ips[i].Unmap()
	}
	if err != nil || !ValidAddresses(ips, policy.AllowPrivate, u.Scheme == "http") {
		return "", nil, badRequest
	}
	address := net.JoinHostPort(host, port)
	u.Host = address
	dialer := &net.Dialer{Timeout: policy.Timeout}
	transport := &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: policy.Timeout, ResponseHeaderTimeout: policy.Timeout,
		IdleConnTimeout: 30 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 4,
		MaxConnsPerHost: 4, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true,
	}
	transport.DialContext = func(ctx context.Context, network, got string) (net.Conn, error) {
		if got != address {
			return nil, bad("unexpected_target")
		}
		return DialPinned(ctx, network, port, ips, dialer.DialContext)
	}
	return u.String(), transport, nil
}

// ValidAddresses accepts an answer only if every address survives policy: no
// invalid or 4-in-6 forms, private targets only when allowed, and non-private
// targets only over TLS at a globally routable address outside the reserved
// blocks that are not safe to reach from the server.
func ValidAddresses(ips []netip.Addr, allowPrivate, plainHTTP bool) bool {
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !ip.IsValid() || ip.Is4In6() {
			return false
		}
		private := ip.IsPrivate() || ip.IsLoopback()
		if private {
			if !allowPrivate {
				return false
			}
			continue
		}
		if plainHTTP || !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
			return false
		}
		for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64"} {
			if netip.MustParsePrefix(block).Contains(ip) {
				return false
			}
		}
		if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
			return false
		}
	}
	return true
}

// DialPinned dials each pinned address in order and returns the first that
// connects. It never dials an address that was not already validated.
func DialPinned(ctx context.Context, network, port string, ips []netip.Addr,
	dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	for _, ip := range ips {
		conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, &Error{Code: "connection_failed", Retryable: true}
}

// String is a helper for diagnostics only; it never appears in a response.
