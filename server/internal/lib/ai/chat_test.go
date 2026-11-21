package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestValidateMessages(t *testing.T) {
	valid := []Message{{Role: "user", Content: "hello"}}
	if err := validateMessages(valid); err != nil {
		t.Errorf("valid message rejected: %v", err)
	}

	// Empty and oversized arrays.
	if validateMessages(nil) == nil {
		t.Error("nil accepted")
	}
	if validateMessages([]Message{}) == nil {
		t.Error("empty accepted")
	}
	if validateMessages(make([]Message, 65)) == nil {
		t.Error("65 messages accepted")
	}

	// Unknown role.
	if validateMessages([]Message{{Role: "unknown", Content: "x"}}) == nil {
		t.Error("unknown role accepted")
	}

	// System message after position 0.
	msgs := []Message{{Role: "user", Content: "a"}, {Role: "system", Content: "b"}}
	if validateMessages(msgs) == nil {
		t.Error("system after position 0 accepted")
	}

	// Tool message without tool_call_id.
	if validateMessages([]Message{{Role: "tool", Content: "result"}}) == nil {
		t.Error("tool without call_id accepted")
	}

	// Valid tool message.
	if validateMessages([]Message{{Role: "tool", Content: "ok", ToolCallID: "call_1"}}) != nil {
		t.Error("valid tool message rejected")
	}

	// Oversized content.
	big := make([]byte, maxMessageBytes+1)
	large := []Message{{Role: "user", Content: string(big)}}
	if validateMessages(large) == nil {
		t.Error("oversized message accepted")
	}

	// Total budget.
	small := make([]byte, maxMessageBytes)
	many := make([]Message, maxMessages)
	for i := range many {
		many[i] = Message{Role: "user", Content: string(small)}
	}
	if validateMessages(many) == nil {
		t.Error("total budget not enforced")
	}
}

func TestOutputAndHistoryBoundaries(t *testing.T) {
	for _, n := range []int{1, 16384} {
		c := config()
		c.MaxTokens = n
		c.MaxResponseBytes = MaxResponseBytes
		g, err := NewGateway(c)
		if err != nil {
			t.Fatal(err)
		}
		for _, requested := range []int{0, 1, n, n + 1} {
			body, err := g.chatBody([]Message{{Role: "user", Content: "hello"}}, ChatOptions{MaxTokens: requested}, false)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				MaxTokens int `json:"max_tokens"`
			}
			_ = json.Unmarshal(body, &wire)
			want := n
			if requested > 0 && requested < n {
				want = requested
			}
			if wire.MaxTokens != want {
				t.Fatalf("limit = %d, want %d", wire.MaxTokens, want)
			}
		}
	}
	for _, n := range []int{-1, 0, 16385} {
		c := config()
		c.MaxTokens = n
		if _, err := NewGateway(c); err == nil {
			t.Fatalf("accepted tokens %d", n)
		}
	}
	c := config()
	c.MaxResponseBytes = MaxResponseBytes + 1
	if _, err := NewGateway(c); err == nil {
		t.Fatal("accepted >1MiB")
	}
	long := strings.Repeat("a", maxCompletionText)
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": long}}}, "usage": map[string]int{"completion_tokens": 16384}})
	result, err := parseCompletion(raw, 16384)
	if err != nil || result.Content != long {
		t.Fatalf("large output: %v", err)
	}
	if _, err := parseCompletion(raw, 16383); err == nil {
		t.Fatal("per-call usage exceeded")
	}
	if err := validateMessages([]Message{{Role: "assistant", Content: result.Content}, {Role: "user", Content: "continue"}}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	msgs := []Message{{Role: "user", Content: strings.Repeat("x", MaxMessageBytes)}, {Role: "assistant", Content: strings.Repeat("x", MaxHistoryBytes-MaxMessageBytes)}}
	if err := validateMessages(msgs); err != nil {
		t.Fatal(err)
	}
	msgs[1].Content += "x"
	if err := validateMessages(msgs); err == nil {
		t.Fatal("history cap not enforced")
	}
}

func TestValidateTools(t *testing.T) {
	valid := []ToolSpec{{Type: "function", Function: ToolFunction{Name: "get_weather", Description: "Get weather", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false}`)}}}
	if err := validateTools(valid, "auto"); err != nil {
		t.Errorf("valid tools rejected: %v", err)
	}

	if validateTools(nil, "") != nil {
		t.Error("nil tools rejected")
	}
	if validateTools(valid, "tool with spaces") == nil {
		t.Error("invalid tool_choice accepted")
	}
	if validateTools(valid, "auto") != nil {
		t.Error("valid tool_choice rejected")
	}

	// Empty name.
	bad := []ToolSpec{{Type: "function", Function: ToolFunction{Name: "", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	if validateTools(bad, "") == nil {
		t.Error("empty tool name accepted")
	}

	// Non-object parameters.
	badParams := []ToolSpec{{Type: "function", Function: ToolFunction{Name: "f", Description: "d", Parameters: json.RawMessage(`"string"`)}}}
	if validateTools(badParams, "") == nil {
		t.Error("non-object params accepted")
	}

	// A bare tool name is a valid choice; it is normalized to the object form.
	if err := validateTools(valid, "get_weather"); err != nil {
		t.Errorf("tool name rejected: %v", err)
	}
}

func TestChatBodyToolChoiceShape(t *testing.T) {
	g, err := NewGateway(config())
	if err != nil {
		t.Fatal(err)
	}
	tools := []ToolSpec{{Type: "function", Function: ToolFunction{Name: "web_search", Description: "Search", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	body, err := g.chatBody([]Message{{Role: "user", Content: "hello"}}, ChatOptions{MaxTokens: 16, Tools: tools, ToolChoice: "web_search"}, true)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		ToolChoice struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ToolChoice.Type != "function" || wire.ToolChoice.Function.Name != "web_search" {
		t.Fatalf("forced tool choice = %+v", wire.ToolChoice)
	}

	body, err = g.chatBody([]Message{{Role: "user", Content: "hello"}}, ChatOptions{MaxTokens: 16, Tools: tools}, true)
	if err != nil {
		t.Fatal(err)
	}
	var mode struct {
		ToolChoice string `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &mode); err != nil {
		t.Fatal(err)
	}
	if mode.ToolChoice != "auto" {
		t.Fatalf("default tool choice = %q, want auto", mode.ToolChoice)
	}
}

func TestCompletionFailurePreservesUsage(t *testing.T) {
	for name, choices := range map[string]string{
		"cap":             `[{"finish_reason":"stop","message":{"content":"secret"}}]`,
		"refusal":         `[{"finish_reason":"stop","message":{"content":"secret","refusal":"no"}}]`,
		"invalid_finish":  `[{"finish_reason":"length","message":{"content":"secret"}}]`,
		"invalid_content": `[{"message":{"content":{}}}]`,
		"missing_choice":  `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			completion := 9
			if name == "cap" {
				completion = 101
			}
			usage, _ := json.Marshal(map[string]int{"prompt_tokens": 7, "completion_tokens": completion})
			raw := `{"choices":` + choices + `,"usage":` + string(usage) + `}`
			assertUsageFailure(t, raw, 7, completion)
		})
	}
}

func TestCompletionFailurePreservesValidCounters(t *testing.T) {
	for _, tc := range []struct {
		usage              string
		prompt, completion int
	}{
		{`{"prompt_tokens":7,"completion_tokens":-1}`, 7, 0},
		{`{"prompt_tokens":"invalid","completion_tokens":9}`, 0, 9},
		{`{"prompt_tokens":7,"completion_tokens":"invalid"}`, 7, 0},
		{`{}`, 0, 0},
	} {
		assertUsageFailure(t, `{"choices":[],"usage":`+tc.usage+`}`, tc.prompt, tc.completion)
	}
}

func assertUsageFailure(t *testing.T, raw string, prompt, completion int) {
	t.Helper()
	want := ChatResult{PromptTokens: prompt, CompletionTokens: completion}
	for _, gateway := range []bool{false, true} {
		var got ChatResult
		var err error
		if gateway {
			got, err = usageTestGateway(raw).Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, ChatOptions{})
		} else {
			got, err = parseCompletion([]byte(raw), 100)
		}
		if !errors.Is(err, ErrOutput) || got.Content != "" || got.FinishReason != "" || len(got.ToolCalls) != 0 || got.PromptTokens != want.PromptTokens || got.CompletionTokens != want.CompletionTokens {
			t.Fatalf("gateway=%v: result=%+v err=%v, want usage=%+v", gateway, got, err, want)
		}
	}
}

type usageTransport func(*http.Request) (*http.Response, error)

func (f usageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func usageTestGateway(body string) *Gateway {
	return &Gateway{config: config(), client: &http.Client{Transport: usageTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
}

func TestParseCompletion(t *testing.T) {
	// Valid stop response.
	raw := `{"choices":[{"finish_reason":"stop","message":{"content":"hello"}}],"usage":{"completion_tokens":5}}`
	r, err := parseCompletion([]byte(raw), 100)
	if err != nil || r.Content != "hello" || r.FinishReason != "stop" {
		t.Errorf("stop parse failed: %v content=%q", err, r.Content)
	}

	// Refusal.
	refusal := `{"choices":[{"finish_reason":"stop","message":{"content":"no","refusal":"I can't"}}],"usage":{"completion_tokens":1}}`
	if _, err := parseCompletion([]byte(refusal), 100); err == nil {
		t.Error("refusal accepted")
	}

	// Tool calls.
	toolRaw := `{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}}],"usage":{"completion_tokens":8}}`
	r, err = parseCompletion([]byte(toolRaw), 100)
	if err != nil || len(r.ToolCalls) != 1 || r.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool_calls parse failed: %v", err)
	}

	// Token over budget.
	big := `{"choices":[{"finish_reason":"stop","message":{"content":"hi"}}],"usage":{"completion_tokens":200}}`
	if _, err := parseCompletion([]byte(big), 100); err == nil {
		t.Error("token over budget accepted")
	}

	// Missing finish reason.
	missing := `{"choices":[{"message":{"content":"hi"}}],"usage":{"completion_tokens":1}}`
	if _, err := parseCompletion([]byte(missing), 100); err == nil {
		t.Error("missing finish_reason accepted")
	}
}
