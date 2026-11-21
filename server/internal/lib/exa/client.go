// Package exa is a minimal client for Exa's hosted MCP endpoint. It speaks the
// JSON-RPC tools/call shape the web-access plugin uses, prefers keyless access,
// and rotates stored API keys only when the keyless attempt is refused.
package exa

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// MCPEndpoint is the hosted MCP server. It is fixed deployment configuration,
// never a value from a request body.
const MCPEndpoint = "https://mcp.exa.ai/mcp"

// MaxKeys bounds how many stored keys one request may rotate through.
const MaxKeys = 20

const (
	MaxResponseBytes = 1 << 20
	maxQueryBytes    = 2048
)

var (
	ErrInput            = errors.New("invalid Exa input")
	ErrOutput           = errors.New("invalid Exa response")
	ErrResponseTooLarge = errors.New("Exa response too large")
	ErrTool             = errors.New("Exa tool reported an error")
)

// StatusError carries only the HTTP status; the provider body never escapes.
type StatusError struct {
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("Exa request failed with status %d", e.Status)
}

// Config is deployment-owned. Keys are plaintext only for the lifetime of a
// request; callers must seal them at rest.
type Config struct {
	Keys             []string
	Timeout          time.Duration
	Endpoint         string
	HTTPClient       *http.Client
	MaxResponseBytes int64
}

// Client performs one search per call, trying keyless access first and then
// each stored key once when the keyless attempt is refused.
type Client struct {
	endpoint string
	keys     []string
	client   *http.Client
	maxBytes int64
	// next spreads key usage round-robin so one key never absorbs every call.
	next atomic.Uint64
}

func New(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 || cfg.Timeout > 120*time.Second {
		return nil, ErrInput
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = MCPEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrInput
	}
	if len(cfg.Keys) > MaxKeys {
		return nil, ErrInput
	}
	keys := make([]string, 0, len(cfg.Keys))
	for _, k := range cfg.Keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 || maxBytes > MaxResponseBytes {
		maxBytes = MaxResponseBytes
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{endpoint: endpoint, keys: keys, client: client, maxBytes: maxBytes}, nil
}

// Search calls web_search_exa and returns the first non-empty text content.
func (c *Client) Search(ctx context.Context, query string, numResults int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > maxQueryBytes {
		return "", ErrInput
	}
	if numResults <= 0 || numResults > 10 {
		numResults = 5
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "web_search_exa",
			"arguments": map[string]any{
				"query":      query,
				"numResults": numResults,
				"livecrawl":  "fallback",
				"type":       "auto",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", ErrInput
	}

	// Keyless first: the public endpoint serves anonymous requests. Only a
	// refusal (401/403/429) justifies spending a stored key.
	text, status, err := c.call(ctx, body, "")
	if err == nil {
		return text, nil
	}
	if !refused(status) || len(c.keys) == 0 {
		return "", err
	}
	start := int(c.next.Add(1)-1) % len(c.keys)
	for i := range c.keys {
		key := c.keys[(start+i)%len(c.keys)]
		text, status, err = c.call(ctx, body, key)
		if err == nil {
			return text, nil
		}
		if !refused(status) {
			return "", err
		}
	}
	return "", err
}

func refused(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests
}

func (c *Client) call(ctx context.Context, body []byte, key string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, ErrInput
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return "", resp.StatusCode, err
	}
	if int64(len(raw)) > c.maxBytes {
		return "", resp.StatusCode, ErrResponseTooLarge
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", resp.StatusCode, &StatusError{Status: resp.StatusCode}
	}
	text, err := parse(raw)
	return text, resp.StatusCode, err
}

// parse accepts either a plain JSON-RPC body or an SSE stream whose `data:`
// lines carry the JSON-RPC message.
func parse(raw []byte) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", ErrOutput
	}
	if trimmed[0] == '{' {
		return decode(trimmed)
	}
	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	scanner.Buffer(make([]byte, 0, 64*1024), MaxResponseBytes)
	var lastErr error
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		text, err := decode([]byte(payload))
		if err == nil {
			return text, nil
		}
		lastErr = err
	}
	if err := scanner.Err(); err != nil {
		return "", ErrOutput
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", ErrOutput
}

type rpcResponse struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decode(raw []byte) (string, error) {
	var rpc rpcResponse
	if err := json.Unmarshal(raw, &rpc); err != nil {
		return "", ErrOutput
	}
	if rpc.Error != nil {
		return "", ErrTool
	}
	if rpc.Result.IsError {
		return "", ErrTool
	}
	for _, item := range rpc.Result.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return item.Text, nil
		}
	}
	return "", ErrOutput
}
