package ai

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrConfig = errors.New("invalid AI configuration")
var ErrProvider = errors.New("AI provider request failed")

// ErrHostNotAllowed means the endpoint's host is outside the deployment's
// egress allowlist. The handler maps it to a distinct, safe 422 message.
var ErrHostNotAllowed = errors.New("endpoint host not allowed")

// Output and byte caps are independent: token counts do not imply a fixed byte size.
const (
	MaxOutputTokens  = 16384
	MaxResponseBytes = 1 << 20
	MaxMessageBytes  = 512 << 10
	MaxHistoryBytes  = 768 << 10
)

// Config is deployment-owned. AllowedHosts must never come from the endpoint's user.
// Endpoint is the exact chat completions URL. No credentials belong in Endpoint.
type Config struct {
	Endpoint         string
	AllowedHosts     []string
	Model            string
	APIKey           string
	Timeout          time.Duration
	MaxTokens        int
	MaxResponseBytes int64
}

type Gateway struct {
	config Config
	client *http.Client
}

// MaxTokens returns the configured token budget. It is used by the harness
// to decide how much context fits and to verify the provider respected the limit.
func (g *Gateway) MaxTokens() int {
	if g == nil {
		return 0
	}
	return g.config.MaxTokens
}

// Model returns the configured model name so a run log can record it without
// duplicating deployment configuration at the call site.
func (g *Gateway) Model() string {
	if g == nil {
		return ""
	}
	return g.config.Model
}

func NewGateway(c Config) (*Gateway, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.Endpoint, "#") {
		return nil, ErrConfig
	}
	host := u.Hostname()
	if !validHostname(host) || strings.HasSuffix(u.Host, ":") {
		return nil, ErrConfig
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, ErrConfig
		}
	}
	allowed := false
	for _, h := range c.AllowedHosts {
		if h == host && h != "" {
			allowed = true
		}
	}
	if !allowed {
		return nil, ErrHostNotAllowed
	}
	if strings.TrimSpace(c.Model) == "" || len(c.Model) > 200 || c.APIKey == "" || strings.ContainsAny(c.APIKey, "\r\n") || len(c.APIKey) > 4096 || c.Timeout <= 0 || c.Timeout > 300*time.Second || c.MaxTokens < 1 || c.MaxTokens > MaxOutputTokens || c.MaxResponseBytes < 1 || c.MaxResponseBytes > MaxResponseBytes {
		return nil, ErrConfig
	}
	c.Endpoint = resolveEndpoint(u)
	tr := &http.Transport{Proxy: nil, DialContext: secureDial, TLSHandshakeTimeout: c.Timeout, ResponseHeaderTimeout: c.Timeout, DisableKeepAlives: true}
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	c.AllowedHosts = nil
	return &Gateway{config: c, client: client}, nil
}

// Every dial resolves afresh, rejects a mixed public/private answer, and connects
// to the checked numeric address. TLS still verifies the configured hostname.
func validHostname(host string) bool {
	if len(host) > 253 || net.ParseIP(host) != nil || host != strings.ToLower(host) {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func secureDial(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{}
	return dialChecked(ctx, network, address, net.DefaultResolver.LookupNetIP, d.DialContext)
}

// resolveEndpoint completes an OpenAI-compatible base path with the chat
// completions route, so "https://host/v1" and "https://host/v1/" reach the same
// URL the provider expects. A full endpoint is returned as configured.
func resolveEndpoint(u *url.URL) string {
	path := strings.TrimSuffix(u.Path, "/")
	if path == "/v1" || path == "" {
		path = "/v1/chat/completions"
	}
	return u.Scheme + "://" + u.Host + path
}

func dialChecked(ctx context.Context, network, address string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrProvider
	}
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, ErrProvider
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, ErrProvider
		}
	}
	for _, ip := range ips {
		conn, e := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, ErrProvider
}

func publicIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// Shared address space and documentation/reserved ranges are not public destinations.
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(s).Contains(ip) {
			return false
		}
	}
	return true
}
