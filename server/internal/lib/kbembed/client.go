// Package kbembed implements the pinned self-hosted BGE-M3 dense embedding profile.
package kbembed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	model            = "BAAI/bge-m3"
	dimensions       = 1024
	requestTimeout   = 15 * time.Second
	maxResponseBytes = 1 << 20
)

// Config is copied by New. Model must be BAAI/bge-m3, Dimensions must be 1024,
// and Revision must be a lowercase 40-hex commit. AllowedHosts contains exact
// hostnames without ports or wildcards. Private/loopback targets (including HTTP)
// require AllowPrivate. All other targets require HTTPS. Credentials are required.
// Zero limits default to 32 inputs, 16 KiB/input, 128 KiB encoded JSON and 15s.
// Limits may only be lowered; Timeout may be raised up to 120s.
// The revision header attests a trusted gateway, not cryptographic weight identity.
type Config struct {
	Endpoint, Model, Revision                    string
	Dimensions                                   int
	Credential                                   string
	AllowedHosts                                 []string
	AllowPrivate                                 bool
	MaxBatchSize, MaxInputBytes, MaxRequestBytes int
	Timeout                                      time.Duration
}

type Client struct {
	profile Config
	http    *http.Client
}

type providerError struct {
	code      string
	retryable bool
}

func (e *providerError) Error() string { return "embedding: " + e.code }

// IsRetryable classifies sanitized provider failures; no retry is performed here.
func IsRetryable(err error) bool {
	var provider *providerError
	return errors.As(err, &provider) && provider.retryable
}

// New constructs a reusable client with a dedicated, proxy-free, pinned transport.
// Endpoint accepts only an origin, /v1, or /v1/embeddings (optional trailing slash).
func New(profile Config) (*Client, error) {
	if profile.MaxBatchSize == 0 {
		profile.MaxBatchSize = 32
	}
	if profile.MaxInputBytes == 0 {
		profile.MaxInputBytes = 16 << 10
	}
	if profile.MaxRequestBytes == 0 {
		profile.MaxRequestBytes = 128 << 10
	}
	if profile.Timeout == 0 {
		profile.Timeout = requestTimeout
	}
	if profile.MaxBatchSize < 1 || profile.MaxBatchSize > 32 ||
		profile.MaxInputBytes < 1 || profile.MaxInputBytes > 16<<10 ||
		profile.MaxRequestBytes < 1 || profile.MaxRequestBytes > 128<<10 ||
		profile.Timeout < time.Millisecond || profile.Timeout > 120*time.Second {
		return nil, &providerError{code: "invalid_profile"}
	}
	if profile.Model != model || profile.Dimensions != dimensions || !validRevision(profile.Revision) || profile.Credential == "" {
		return nil, &providerError{code: "invalid_profile"}
	}
	for _, r := range profile.Credential {
		if r < 0x21 || r > 0x7e {
			return nil, &providerError{code: "invalid_profile"}
		}
	}
	endpoint, transport, err := newTransport(profile)
	if err != nil {
		return nil, err
	}
	profile.Endpoint = endpoint
	profile.AllowedHosts = append([]string(nil), profile.AllowedHosts...)
	return &Client{profile: profile, http: &http.Client{
		Transport: transport, Timeout: profile.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func validRevision(revision string) bool {
	if len(revision) != 40 {
		return false
	}
	for _, r := range revision {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// Embed preserves input bytes for both queries and documents, without prefixes.
// Token limits are checked by the pinned service's tokenizer; never truncated.
func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if c == nil || c.http == nil || ctx == nil || len(inputs) == 0 || len(inputs) > c.profile.MaxBatchSize {
		return nil, &providerError{code: "invalid_input"}
	}
	for _, input := range inputs {
		if !utf8.ValidString(input) || len(input) > c.profile.MaxInputBytes || strings.TrimSpace(input) == "" {
			return nil, &providerError{code: "invalid_input"}
		}
	}
	payload, err := json.Marshal(struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		EncodingFormat string   `json:"encoding_format"`
		Dimensions     int      `json:"dimensions"`
	}{model, inputs, "float", dimensions})
	if err != nil || len(payload) > c.profile.MaxRequestBytes {
		return nil, &providerError{code: "invalid_input"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.profile.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, &providerError{code: "invalid_request"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Embedding-Revision", c.profile.Revision)
	if c.profile.Credential != "" {
		req.Header.Set("Authorization", "Bearer "+c.profile.Credential)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			return nil, &providerError{code: "rate_limited", retryable: true}
		case resp.StatusCode >= 500 && resp.StatusCode <= 599:
			return nil, &providerError{code: "unavailable", retryable: true}
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return nil, &providerError{code: "authentication"}
		default:
			return nil, &providerError{code: "rejected"}
		}
	}
	revisions := resp.Header.Values("X-Embedding-Revision")
	if len(revisions) != 1 || revisions[0] != c.profile.Revision {
		return nil, &providerError{code: "revision_mismatch"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, transportError(err)
	}
	if len(body) > maxResponseBytes {
		return nil, &providerError{code: "response_too_large"}
	}
	return decodeVectors(body, len(inputs))
}

func transportError(err error) error {
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return &providerError{code: "timeout", retryable: true}
	}
	return &providerError{code: "transport"}
}
