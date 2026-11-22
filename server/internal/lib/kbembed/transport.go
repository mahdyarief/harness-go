package kbembed

import (
	"net/http"
	"net/url"
	"strings"

	"hsg/internal/lib/egress"
)

// newTransport builds a pinned, proxy-free transport for the embedding
// endpoint. The egress guard lives in lib/egress and is shared with the
// harness HTTP tool; this function keeps the embedding-specific endpoint
// shape check (allowed host, /v1/embeddings path) and maps egress rejections
// onto the sanitized provider error.
func newTransport(config Config) (string, *http.Transport, error) {
	bad := &providerError{code: "invalid_endpoint"}
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.RawPath != "" || u.Opaque != "" || strings.Contains(config.Endpoint, "#") ||
		(u.Scheme != "https" && u.Scheme != "http") {
		return "", nil, bad
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path != "" && path != "/v1" && path != "/v1/embeddings" {
		return "", nil, bad
	}
	u.Path = "/v1/embeddings"
	endpoint, transport, err := egress.New(egress.Policy{
		AllowedHosts: config.AllowedHosts,
		AllowPrivate: config.AllowPrivate,
		Timeout:      config.Timeout,
	}, u.String())
	if err != nil {
		return "", nil, &providerError{code: egress.Code(err), retryable: egress.IsRetryable(err)}
	}
	return endpoint, transport, nil
}
