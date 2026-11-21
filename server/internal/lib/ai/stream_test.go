package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// sse joins lines into an SSE body. A blank line between frames is optional for
// the parser, so tests stay readable.
func sse(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func collectDeltas(t *testing.T, body string) (ChatResult, string) {
	t.Helper()
	var text strings.Builder
	result, err := readEventStream(strings.NewReader(body), 65536, 512, func(d StreamDelta) error {
		if d.Done {
			return nil
		}
		text.WriteString(d.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("readEventStream: %v", err)
	}
	return result, text.String()
}

func TestLargeStreamOutput(t *testing.T) {
	text := strings.Repeat("a", 200000)
	chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}, "finish_reason": "stop"}}, "usage": map[string]int{"completion_tokens": 16384}})
	wire := "data: " + string(chunk) + "\n\ndata: [DONE]\n\n"
	var delivered strings.Builder
	result, err := readEventStream(strings.NewReader(wire), MaxResponseBytes, 16384, func(d StreamDelta) error { delivered.WriteString(d.Content); return nil })
	if err != nil || result.Content != text || delivered.String() != text {
		t.Fatalf("large stream: %v", err)
	}
	if _, err := readEventStream(strings.NewReader(wire), MaxResponseBytes, 16383, func(StreamDelta) error { return nil }); err == nil {
		t.Fatal("per-call stream usage exceeded")
	}
	if _, err := readEventStream(strings.NewReader(wire), int64(len(chunk)), 16384, func(StreamDelta) error { return nil }); err == nil {
		t.Fatal("wire limit exceeded")
	}
}

func TestReadEventStreamAssemblesText(t *testing.T) {
	body := sse(
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		`data: {"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`,
		`data: [DONE]`,
	)
	result, text := collectDeltas(t, body)
	if text != "Hello" {
		t.Fatalf("text = %q, want Hello", text)
	}
	if result.Content != "Hello" || result.FinishReason != "stop" {
		t.Fatalf("result = %+v", result)
	}
	if result.PromptTokens != 7 || result.CompletionTokens != 2 {
		t.Fatalf("usage = %d/%d, want 7/2", result.PromptTokens, result.CompletionTokens)
	}
}

func TestReadEventStreamAssemblesToolCalls(t *testing.T) {
	// Arguments arrive split across chunks, as real providers do.
	body := sse(
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_categories","arguments":"{\"a\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`data: {"choices":[{"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	result, text := collectDeltas(t, body)
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
	}
	call := result.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "list_categories" || call.Function.Arguments != `{"a":1}` {
		t.Fatalf("call = %+v", call)
	}
	if result.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", result.FinishReason)
	}
}

func TestReadEventStreamRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"missing done":    sse(`data: {"choices":[{"delta":{"content":"x"}}]}`),
		"bad json":        sse(`data: {not json}`, `data: [DONE]`),
		"refusal":         sse(`data: {"choices":[{"delta":{"refusal":"no"}}]}`, `data: [DONE]`),
		"tokens over cap": sse(`data: {"choices":[{"delta":{}}],"usage":{"completion_tokens":999}}`, `data: [DONE]`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := readEventStream(strings.NewReader(body), 65536, 512, func(StreamDelta) error { return nil })
			if err == nil {
				t.Fatal("malformed stream accepted")
			}
			if !errors.Is(err, ErrOutput) {
				t.Fatalf("err = %v, want ErrOutput", err)
			}
		})
	}
}

func TestReadEventStreamRespectsByteCap(t *testing.T) {
	body := sse(
		`data: {"choices":[{"delta":{"content":"a long chunk of text"}}]}`,
		`data: [DONE]`,
	)
	_, err := readEventStream(strings.NewReader(body), 8, 512, func(StreamDelta) error { return nil })
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
}

func TestStreamFailurePreservesOnlyKnownUsage(t *testing.T) {
	usage := `data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":9}}`
	stop := errors.New("sink failed")
	for _, tc := range []struct {
		name, body         string
		want               error
		prompt, completion int
		sink               bool
	}{
		{"cap", sse(`data: {"choices":[{"delta":{"content":"secret"}}],"usage":{"prompt_tokens":7,"completion_tokens":101}}`), ErrOutput, 7, 101, false},
		{"refusal", sse(`data: {"choices":[{"delta":{"content":"secret","refusal":"no"}}],"usage":{"prompt_tokens":7,"completion_tokens":9}}`), ErrOutput, 7, 9, false},
		{"invalid_schema", sse(`data: {"choices":[{"delta":{"content":{}}}],"usage":{"prompt_tokens":7,"completion_tokens":9}}`), ErrOutput, 7, 9, false},
		{"malformed_after_usage", sse(usage, `data: {broken`), ErrOutput, 7, 9, false},
		{"truncated", sse(usage), ErrOutput, 7, 9, false},
		{"repeated_usage", sse(usage, usage), ErrOutput, 7, 9, false},
		{"invalid_finish", sse(usage, `data: {"choices":[{"delta":{"content":"secret"},"finish_reason":"length"}]}`), ErrOutput, 7, 9, false},
		{"unknown_usage", sse(`data: {broken`), ErrOutput, 0, 0, false},
		{"sink_after_usage", sse(usage, `data: {"choices":[{"delta":{"content":"x"}}]}`, `data: [DONE]`), stop, 7, 9, true},
		{"sink_at_done", sse(usage, `data: [DONE]`), stop, 7, 9, true},
		{"sink_before_usage", sse(`data: {"choices":[{"delta":{"content":"x"}}]}`, usage), stop, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, gateway := range []bool{false, true} {
				var delivered string
				sink := func(d StreamDelta) error {
					if tc.sink {
						return stop
					}
					delivered += d.Content
					return nil
				}
				var got ChatResult
				var err error
				if gateway {
					got, err = usageTestGateway(tc.body).ChatStream(context.Background(), []Message{{Role: "user", Content: "x"}}, ChatOptions{}, sink)
				} else {
					got, err = readEventStream(strings.NewReader(tc.body), 4096, 100, sink)
				}
				if !errors.Is(err, tc.want) || got.PromptTokens != tc.prompt || got.CompletionTokens != tc.completion || got.Content != "" || got.FinishReason != "" || len(got.ToolCalls) != 0 || delivered != "" {
					t.Fatalf("gateway=%v: result=%+v err=%v delivered=%q", gateway, got, err, delivered)
				}
			}
		})
	}
}

type failingUsageReader struct{}

func (failingUsageReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestStreamRetainsUsageAcrossReadLimits(t *testing.T) {
	prefix := sse(`data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":9}}`)
	for _, tc := range []struct {
		name  string
		body  io.Reader
		limit int64
		want  error
	}{
		{"read", io.MultiReader(strings.NewReader(prefix), failingUsageReader{}), 4096, ErrProvider},
		{"bytes", strings.NewReader(prefix + sse(`data: [DONE]`)), int64(len(prefix)), ErrResponseTooLarge},
		{"line", strings.NewReader(prefix + strings.Repeat("x", maxSSELineBytes+1)), 1 << 20, ErrProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readEventStream(tc.body, tc.limit, 100, func(StreamDelta) error { return nil })
			if !errors.Is(err, tc.want) || got.PromptTokens != 7 || got.CompletionTokens != 9 || got.Content != "" || len(got.ToolCalls) != 0 {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
}

func TestReadEventStreamAbortsOnSinkError(t *testing.T) {
	stop := errors.New("client gone")
	body := sse(
		`data: {"choices":[{"delta":{"content":"x"}}]}`,
		`data: [DONE]`,
	)
	_, err := readEventStream(strings.NewReader(body), 65536, 512, func(StreamDelta) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the sink error", err)
	}
}

func TestReadEventStreamCapsAssembledText(t *testing.T) {
	var lines []string
	for i := 0; i < maxCompletionText/8192+1; i++ {
		lines = append(lines, `data: {"choices":[{"delta":{"content":"`+strings.Repeat("a", 8192)+`"}}]}`)
	}
	lines = append(lines, `data: [DONE]`)
	_, err := readEventStream(strings.NewReader(sse(lines...)), 1<<20, 512, func(StreamDelta) error { return nil })
	if !errors.Is(err, ErrOutput) {
		t.Fatalf("err = %v, want ErrOutput", err)
	}
}

func TestSSEPayload(t *testing.T) {
	for line, want := range map[string]string{
		"data: [DONE]":                       "[DONE]",
		"data:{\"a\":1}":                     `{"a":1}`,
		": comment":                          "",
		"event: message":                     "",
		"data:":                              "",
		"data:   {\"a\": 1}  ":               `{"a": 1}`,
		"data: {\"choices\":[]}":             `{"choices":[]}`,
		"data: {\"x\":\"  spaced  \"}":       `{"x":"  spaced  "}`,
		"data: {\"text\":\"line1\\nline2\"}": `{"text":"line1\nline2"}`,
	} {
		got, ok := ssePayload(line)
		if want == "" {
			if ok {
				t.Errorf("%q accepted, want ignored", line)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q = %q/%v, want %q", line, got, ok, want)
		}
	}
}
