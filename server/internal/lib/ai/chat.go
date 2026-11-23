package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// ErrInput marks a request the gateway refuses to send: unknown role, oversized
// content, or a tool schema that is not a JSON object. It never carries content.
var ErrInput = errors.New("invalid AI input")

const (
	maxMessages          = 64
	maxMessageBytes      = MaxMessageBytes
	maxTotalMessageBytes = MaxHistoryBytes
	maxToolSpecs         = 16
	maxToolDescription   = 1024
	maxToolSchemaBytes   = 8192
	maxCompletionText    = MaxMessageBytes
	maxSSELineBytes      = 256 * 1024
)

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Message is one OpenAI-compatible chat message. Assistant turns that ask for
// tools carry ToolCalls; tool turns answer a specific call via ToolCallID.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec is the schema the model sees. Parameters must be a JSON object.
type ToolSpec struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatOptions struct {
	MaxTokens int
	Tools     []ToolSpec
	// ToolChoice is "auto", "none", "required", or a specific tool name.
	ToolChoice string
	// Temperature and Seed are sent only when set. A nil pointer keeps the
	// provider default; sending zero would silently force greedy or seedless
	// sampling on providers whose defaults differ.
	Temperature *float64 `json:"temperature,omitempty"`
	Seed        *int     `json:"seed,omitempty"`
}

type ChatResult struct {
	Content          string
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	ToolCalls        []ToolCall
}

// ProviderError carries only the HTTP status, never the provider's body, so a
// caller can decide whether a retry is safe without leaking response content.
type ProviderError struct {
	Status    int
	Code      string
	Retryable bool
}

func (e *ProviderError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("AI provider request failed (status %d)", e.Status)
	}
	return "AI provider request failed"
}
func (e *ProviderError) Unwrap() error { return ErrProvider }

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusInternalServerError ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// statusError classifies a non-200 response using a bounded body slice. The
// body is discarded immediately: ProviderError never carries provider text.
func statusError(resp *http.Response) *ProviderError {
	const detailLimit = 4096
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, detailLimit))
	status := resp.StatusCode
	code := ClassifyHTTP(status, string(detail))
	return &ProviderError{
		Status:    status,
		Code:      string(code),
		Retryable: retryableStatus(status) || DefaultRetryableCodes[string(code)],
	}
}

// validateMessages rejects everything the gateway cannot safely forward. The
// budget is measured on the raw content, so a caller cannot smuggle a large
// payload through many small messages.
func validateMessages(messages []Message) error {
	if len(messages) == 0 || len(messages) > maxMessages {
		return ErrInput
	}
	total := 0
	for i, m := range messages {
		if len(m.Content) > maxMessageBytes {
			return ErrInput
		}
		total += len(m.Content)
		if total > maxTotalMessageBytes {
			return ErrInput
		}
		switch m.Role {
		case "system":
			// Only the first message may be a system prompt: later ones would let
			// untrusted text restate the instructions.
			if i != 0 || len(m.ToolCalls) > 0 || m.ToolCallID != "" {
				return ErrInput
			}
		case "user":
			if len(m.ToolCalls) > 0 || m.ToolCallID != "" {
				return ErrInput
			}
		case "assistant":
			if m.ToolCallID != "" {
				return ErrInput
			}
			for _, call := range m.ToolCalls {
				if !toolNamePattern.MatchString(call.ID) || call.Type != "function" ||
					!toolNamePattern.MatchString(call.Function.Name) || len(call.Function.Arguments) > maxToolSchemaBytes {
					return ErrInput
				}
			}
		case "tool":
			if m.ToolCallID == "" || !toolNamePattern.MatchString(m.ToolCallID) || len(m.ToolCalls) > 0 {
				return ErrInput
			}
		default:
			return ErrInput
		}
	}
	return nil
}

func validateTools(tools []ToolSpec, choice string) error {
	if len(tools) > maxToolSpecs {
		return ErrInput
	}
	if choice != "" && choice != "auto" && choice != "none" && choice != "required" &&
		!toolNamePattern.MatchString(choice) {
		return ErrInput
	}
	for _, t := range tools {
		if t.Type != "function" || !toolNamePattern.MatchString(t.Function.Name) ||
			strings.TrimSpace(t.Function.Description) == "" || len(t.Function.Description) > maxToolDescription ||
			len(t.Function.Parameters) == 0 || len(t.Function.Parameters) > maxToolSchemaBytes {
			return ErrInput
		}
		var params map[string]json.RawMessage
		if err := json.Unmarshal(t.Function.Parameters, &params); err != nil || params == nil {
			return ErrInput
		}
	}
	return nil
}

// chatBody builds the provider request. stream is a separate parameter because
// the streaming and blocking responses use the same shape otherwise.
func (g *Gateway) outputLimit(requested int) int {
	if requested <= 0 || requested > g.config.MaxTokens {
		return g.config.MaxTokens
	}
	return requested
}

func (g *Gateway) chatBody(messages []Message, opts ChatOptions, stream bool) ([]byte, error) {
	maxTokens := g.outputLimit(opts.MaxTokens)
	request := map[string]any{"model": g.config.Model, "max_tokens": maxTokens, "messages": messages}
	if stream {
		request["stream"] = true
	}
	if opts.Temperature != nil {
		request["temperature"] = *opts.Temperature
	}
	if opts.Seed != nil {
		request["seed"] = *opts.Seed
	}
	if len(opts.Tools) > 0 {
		request["tools"] = opts.Tools
		choice := opts.ToolChoice
		if choice == "" {
			choice = "auto"
		}
		// Providers that follow OpenAI's shape accept either a mode string or
		// an object naming one function; a bare tool name is normalized here so
		// callers do not have to know the wire format.
		if choice != "auto" && choice != "none" && choice != "required" {
			request["tool_choice"] = map[string]any{
				"type":     "function",
				"function": map[string]string{"name": choice},
			}
		} else {
			request["tool_choice"] = choice
		}
	}
	body, err := json.Marshal(request)
	if len(body) > MaxResponseBytes {
		return nil, ErrInput
	}
	return body, err
}

func (g *Gateway) newChatRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrProvider
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.config.APIKey)
	return req, nil
}

// ErrResponseTooLarge means the provider exceeded the configured response cap.
var ErrResponseTooLarge = errors.New("AI response too large")

// Chat performs one blocking completion. The response is size-capped and the
// token budget is re-checked, so a provider cannot ignore max_tokens silently.
func (g *Gateway) Chat(ctx context.Context, messages []Message, opts ChatOptions) (ChatResult, error) {
	var empty ChatResult
	if g == nil || g.client == nil {
		return empty, ErrConfig
	}
	if err := validateMessages(messages); err != nil {
		return empty, err
	}
	if err := validateTools(opts.Tools, opts.ToolChoice); err != nil {
		return empty, err
	}
	body, err := g.chatBody(messages, opts, false)
	if err != nil {
		return empty, ErrInput
	}
	ctx, cancel := context.WithTimeout(ctx, g.config.Timeout)
	defer cancel()
	req, err := g.newChatRequest(ctx, body)
	if err != nil {
		return empty, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return empty, providerTransportError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return empty, statusError(resp)
	}
	raw, err := readCapped(resp.Body, g.config.MaxResponseBytes)
	if err != nil {
		return empty, err
	}
	return parseCompletion(raw, g.outputLimit(opts.MaxTokens))
}

func providerTransportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &ProviderError{Code: string(CodeTransport), Retryable: true}
}

func readCapped(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, ErrProvider
	}
	if int64(len(raw)) > limit {
		return nil, ErrResponseTooLarge
	}
	return raw, nil
}

type completionEnvelope struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string     `json:"content"`
			Refusal   *string    `json:"refusal"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// usageOnly keeps known accounting on failure without exposing rejected output.
func (r ChatResult) usageOnly() ChatResult {
	return ChatResult{
		PromptTokens:     max(0, r.PromptTokens),
		CompletionTokens: max(0, r.CompletionTokens),
	}
}

// reportedUsage reads accounting independently of the content schema. Invalid
// JSON or invalid counters are not estimates and must not create usage.
func reportedUsage(raw []byte) ChatResult {
	var env struct {
		Usage struct {
			PromptTokens     json.RawMessage `json:"prompt_tokens"`
			CompletionTokens json.RawMessage `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return ChatResult{}
	}
	var usage ChatResult
	// One malformed counter must not erase the other known counter.
	if json.Unmarshal(env.Usage.PromptTokens, &usage.PromptTokens) != nil {
		usage.PromptTokens = 0
	}
	if json.Unmarshal(env.Usage.CompletionTokens, &usage.CompletionTokens) != nil {
		usage.CompletionTokens = 0
	}
	return usage.usageOnly()
}

// parseCompletion validates content while retaining known usage on rejection.
func parseCompletion(raw []byte, maxTokens int) (ChatResult, error) {
	usage := reportedUsage(raw)
	var env completionEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || len(env.Choices) != 1 {
		return usage, ErrOutput
	}
	choice := env.Choices[0]
	if choice.Message.Refusal != nil || len(choice.Message.Content) > maxCompletionText {
		return usage, ErrOutput
	}
	switch choice.FinishReason {
	case "stop":
		if len(choice.Message.ToolCalls) > 0 {
			return usage, ErrOutput
		}
	case "tool_calls":
		if len(choice.Message.ToolCalls) == 0 {
			return usage, ErrOutput
		}
	default:
		return usage, ErrOutput
	}
	if env.Usage.PromptTokens < 0 || env.Usage.CompletionTokens < 0 || env.Usage.CompletionTokens > maxTokens {
		return usage, ErrOutput
	}
	return ChatResult{
		Content:          choice.Message.Content,
		FinishReason:     choice.FinishReason,
		PromptTokens:     env.Usage.PromptTokens,
		CompletionTokens: env.Usage.CompletionTokens,
		ToolCalls:        choice.Message.ToolCalls,
	}, nil
}

// StreamDelta is one incremental chunk handed to the caller's sink.
type StreamDelta struct {
	Content string
	// Done is set on the final chunk after [DONE]; Content is empty then.
	Done bool
}

// ChatStream performs a streaming completion. onDelta is called for every text
// chunk and may return an error to abort (for example, when the client has
// disconnected). The returned ChatResult holds the assembled answer, including
// any tool calls the model asked for.
func (g *Gateway) ChatStream(ctx context.Context, messages []Message, opts ChatOptions, onDelta func(StreamDelta) error) (ChatResult, error) {
	var empty ChatResult
	if g == nil || g.client == nil {
		return empty, ErrConfig
	}
	if onDelta == nil {
		return empty, ErrInput
	}
	if err := validateMessages(messages); err != nil {
		return empty, err
	}
	if err := validateTools(opts.Tools, opts.ToolChoice); err != nil {
		return empty, err
	}
	body, err := g.chatBody(messages, opts, true)
	if err != nil {
		return empty, ErrInput
	}
	ctx, cancel := context.WithTimeout(ctx, g.config.Timeout)
	defer cancel()
	req, err := g.newChatRequest(ctx, body)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := g.client.Do(req)
	if err != nil {
		return empty, providerTransportError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return empty, statusError(resp)
	}
	return readEventStream(resp.Body, g.config.MaxResponseBytes, g.outputLimit(opts.MaxTokens), onDelta)
}

// readEventStream parses OpenAI-compatible SSE: one `data: {json}` payload per
// line, terminated by `data: [DONE]`. The accumulated text is capped at the
// same byte budget as a blocking response, so a stream cannot outgrow it.
func readEventStream(body io.Reader, limit int64, maxTokens int, onDelta func(StreamDelta) error) (ChatResult, error) {
	reader := bufio.NewReaderSize(body, 8192)
	var result ChatResult
	var text strings.Builder
	var consumed int64
	toolCalls := map[int]*ToolCall{}
	var done bool

	for {
		line, err := readLine(reader, maxSSELineBytes)
		if err != nil && !errors.Is(err, io.EOF) {
			return result.usageOnly(), ErrProvider
		}
		if len(line) > 0 {
			consumed += int64(len(line))
			if consumed > limit {
				return result.usageOnly(), ErrResponseTooLarge
			}
			payload, ok := ssePayload(line)
			if ok {
				if payload == "[DONE]" {
					done = true
					break
				}
				if err := applyChunk(payload, &result, &text, toolCalls, maxTokens, onDelta); err != nil {
					return result.usageOnly(), err
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if !done {
		// A stream that ends without [DONE] is truncated: treat it as invalid.
		return result.usageOnly(), ErrOutput
	}
	result.Content = text.String()
	for _, call := range toolCalls {
		result.ToolCalls = append(result.ToolCalls, *call)
	}
	if len(result.ToolCalls) > 0 {
		if result.FinishReason == "" {
			result.FinishReason = "tool_calls"
		}
	}
	if result.FinishReason == "" {
		result.FinishReason = "stop"
	}
	if err := onDelta(StreamDelta{Done: true}); err != nil {
		return result.usageOnly(), err
	}
	return result, nil
}

func readLine(r *bufio.Reader, max int) (string, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > max {
			return "", ErrResponseTooLarge
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return strings.TrimRight(string(buf), "\r\n"), err
		}
		return strings.TrimRight(string(buf), "\r\n"), nil
	}
}

func ssePayload(line string) (string, bool) {
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	return payload, payload != ""
}

type streamChunk struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Content   string  `json:"content"`
			Refusal   *string `json:"refusal"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func applyChunk(payload string, result *ChatResult, text *strings.Builder, calls map[int]*ToolCall, maxTokens int, onDelta func(StreamDelta) error) error {
	raw := []byte(payload)
	usage := reportedUsage(raw)
	// Usage fields are cumulative snapshots for this call, not per-chunk deltas.
	// Keep a known snapshot even if the same frame has an invalid content schema.
	result.PromptTokens = max(result.PromptTokens, usage.PromptTokens)
	result.CompletionTokens = max(result.CompletionTokens, usage.CompletionTokens)
	var chunk streamChunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return ErrOutput
	}
	if chunk.Usage != nil && (chunk.Usage.PromptTokens < 0 || chunk.Usage.CompletionTokens < 0 || chunk.Usage.CompletionTokens > maxTokens) {
		return ErrOutput
	}
	// Validate the entire frame before forwarding any of its content.
	contentBytes := text.Len()
	for _, choice := range chunk.Choices {
		contentBytes += len(choice.Delta.Content)
		if choice.Delta.Refusal != nil || contentBytes > maxCompletionText {
			return ErrOutput
		}
		if choice.FinishReason != "" && choice.FinishReason != "stop" && choice.FinishReason != "tool_calls" {
			return ErrOutput
		}
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			text.WriteString(choice.Delta.Content)
			// Forward the chunk before the next read so the caller can render
			// it live; the assembled text above is the final answer.
			if err := onDelta(StreamDelta{Content: choice.Delta.Content}); err != nil {
				return err
			}
		}
		for _, tc := range choice.Delta.ToolCalls {
			call, ok := calls[tc.Index]
			if !ok {
				call = &ToolCall{Type: "function"}
				calls[tc.Index] = call
			}
			if tc.ID != "" {
				call.ID = tc.ID
			}
			if tc.Function.Name != "" {
				call.Function.Name = tc.Function.Name
			}
			call.Function.Arguments += tc.Function.Arguments
		}
		if choice.FinishReason != "" {
			result.FinishReason = choice.FinishReason
		}
	}
	return nil
}
