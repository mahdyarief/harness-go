package kbembed

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func newTransport(config Config) (string, *http.Transport, error) {
	bad := &providerError{code: "invalid_endpoint"}
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.RawPath != "" || u.Opaque != "" || strings.Contains(config.Endpoint, "#") ||
		(u.Scheme != "https" && u.Scheme != "http") {
		return "", nil, bad
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, entry := range config.AllowedHosts {
		if strings.EqualFold(entry, host) {
			allowed = true
		}
	}
	if !allowed {
		return "", nil, bad
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path != "" && path != "/v1" && path != "/v1/embeddings" {
		return "", nil, bad
	}
	u.Path = "/v1/embeddings"
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return "", nil, bad
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	for i := range ips {
		ips[i] = ips[i].Unmap()
	}
	if err != nil || !validAddresses(ips, config.AllowPrivate, u.Scheme == "http") {
		return "", nil, bad
	}
	address := net.JoinHostPort(host, port)
	u.Host = address
	dialer := &net.Dialer{Timeout: config.Timeout}
	transport := &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: config.Timeout, ResponseHeaderTimeout: config.Timeout,
		IdleConnTimeout: 30 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 4,
		MaxConnsPerHost: 4, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true,
	}
	transport.DialContext = func(ctx context.Context, network, target string) (net.Conn, error) {
		if target != address {
			return nil, bad
		}
		return dialPinned(ctx, network, port, ips, dialer.DialContext)
	}
	return u.String(), transport, nil
}

// Every answer is validated before any dial; addresses stay pinned for the client
// lifetime, so DNS rebinding cannot redirect a later request or leak credentials.
func validAddresses(ips []netip.Addr, allowPrivate, plainHTTP bool) bool {
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

func dialPinned(ctx context.Context, network, port string, ips []netip.Addr,
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
	return nil, &providerError{code: "connection_failed", retryable: true}
}
