// Package httptool turns one pinned egress endpoint into a harness tool.
// The response body never reaches the model raw: every byte passes through
// the caller's Map function, and a service failure surfaces as a
// SERVICE_UNAVAILABLE tool result rather than a run error, so the agent can
// treat the data as unavailable and continue.
package httptool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"hsg/internal/harness/services"
	"hsg/internal/lib/egress"
)

// MaxResponseBytes caps how much of a response is read before the body is
// discarded. A hostile or broken service cannot exhaust memory through the
// tool path.
const MaxResponseBytes = 1 << 20

// Spec describes one external HTTP JSON service.
type Spec struct {
	Name        string
	Description string
	// Params is the JSON schema of the tool arguments. It is passed to the
	// model verbatim and validated by the harness pipeline.
	Params json.RawMessage
	// URL is the pinned endpoint. The host must be allowlisted by the
	// egress policy; the URL may not carry credentials or a query string.
	URL string
	// Method defaults to GET. POST is allowed for parameterized lookups.
	Method string
	// Timeout bounds one attempt, including the pinned dial. Zero means 15s.
	Timeout time.Duration
	// Retries is the number of attempts after the first failure for
	// retryable egress errors. Zero means no retry.
	Retries int
	// AllowPlainHTTP relaxes the TLS requirement for dev environments. It
	// never relaxes the host allowlist or the private-address guard.
	AllowPlainHTTP bool
	AllowPrivate   bool
	AllowedHosts   []string
	// Map turns a successful response into a tool result. It receives the
	// decoded JSON document and must produce the model-facing output. A
	// service-shaped error (upstream 4xx/5xx) should be returned as a
	// services.ToolResult with a short model-safe message.
	Map func(status int, body json.RawMessage) (services.ToolResult, error)
}

// New builds the tool. The endpoint is resolved and pinned once, at mount;
// a policy rejection fails here, not on the first model call.
func New(spec Spec) (services.Tool, error) {
	if spec.Name == "" {
		return nil, fmt.Errorf("httptool: nama alat wajib")
	}
	if spec.Map == nil {
		return nil, fmt.Errorf("httptool: Map wajib")
	}
	if spec.Timeout <= 0 {
		spec.Timeout = 15 * time.Second
	}
	if spec.Method == "" {
		spec.Method = http.MethodGet
	}
	spec.Method = strings.ToUpper(spec.Method)
	if spec.Method != http.MethodGet && spec.Method != http.MethodPost {
		return nil, fmt.Errorf("httptool: metode %q tidak didukung", spec.Method)
	}
	policy := egress.Policy{
		AllowedHosts: spec.AllowedHosts,
		AllowPrivate: spec.AllowPrivate,
		Timeout:      spec.Timeout,
	}
	_, transport, err := egress.New(policy, spec.URL)
	if err != nil {
		return nil, fmt.Errorf("httptool %s: %w", spec.Name, err)
	}
	return &tool{spec: spec, client: &http.Client{
		Transport: transport,
		Timeout:   spec.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

type tool struct {
	spec   Spec
	client *http.Client
}

func (t *tool) Name() string { return t.spec.Name }
func (t *tool) Description() string {
	if t.spec.Description == "" {
		return "Panggil layanan eksternal " + t.spec.Name + "."
	}
	return t.spec.Description
}
func (t *tool) Parameters() json.RawMessage { return t.spec.Params }

// ReadOnly reports that the tool never mutates harness state. A POST still
// only reads from the external service's point of view.
func (t *tool) ReadOnly() bool { return true }

// unavailable is the model-facing result for a service the agent cannot
// reach. It is a tool result, not a run error: the run continues with the
// knowledge that this data source is down.
func unavailable(reason string) services.ToolResult {
	raw, _ := json.Marshal(map[string]any{"error": "SERVICE_UNAVAILABLE", "reason": reason})
	return services.ToolResult{Output: string(raw)}
}

// Execute performs the pinned request. Transport and policy failures map to
// the SERVICE_UNAVAILABLE result; only programming errors (bad arguments,
// a broken Map) return as errors.
func (t *tool) Execute(ctx context.Context, args json.RawMessage) (services.ToolResult, error) {
	var payload any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			return services.ToolResult{}, fmt.Errorf("argumen tidak valid: %w", err)
		}
	}
	var body io.Reader
	if t.spec.Method == http.MethodPost && payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return services.ToolResult{}, fmt.Errorf("argumen tidak valid: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	attempts := t.spec.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return unavailable("waktu tunggu habis"), nil
			case <-time.After(backoff(attempt)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, t.spec.Method, t.spec.URL, body)
		if err != nil {
			return services.ToolResult{}, fmt.Errorf("permintaan tidak valid: %w", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := t.client.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return unavailable("waktu tunggu habis"), nil
			}
			if !egress.IsRetryable(err) {
				break
			}
			continue
		}
		return t.finish(resp)
	}
	_ = lastErr
	return unavailable("layanan tidak dapat dihubungi"), nil
}

// finish reads at most MaxResponseBytes, decodes JSON, and hands the result
// to Map. A non-2xx status still reaches Map: the service is up, its answer
// is a refusal or an error the caller may want to phrase for the model.
func (t *tool) finish(resp *http.Response) (services.ToolResult, error) {
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, MaxResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return unavailable("respons tidak dapat dibaca"), nil
	}
	if len(raw) > MaxResponseBytes {
		return unavailable("respons terlalu besar"), nil
	}
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	if !json.Valid(raw) {
		return unavailable("respons bukan JSON"), nil
	}
	result, err := t.spec.Map(resp.StatusCode, json.RawMessage(raw))
	if err != nil {
		return services.ToolResult{}, err
	}
	return result, nil
}

// backoff is a deterministic, jitter-free pause between retries: attempt 1
// waits 250ms, attempt 2 waits 500ms, and so on, capped at 2s.
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt-1)) * 250 * time.Millisecond
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}
