package agentloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type truncTool struct{ result string }

func (t truncTool) Name() string        { return "echo" }
func (t truncTool) Description() string { return "echo" }
func (t truncTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t truncTool) ReadOnly() bool { return true }
func (t truncTool) Execute(_ context.Context, _ json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{Output: t.result}, nil
}

type truncTools struct{ tool truncTool }

func (t truncTools) Lookup(name string) (services.Tool, bool) {
	if name == t.tool.Name() {
		return t.tool, true
	}
	return nil, false
}
func (t truncTools) Specs() []ai.ToolSpec {
	return []ai.ToolSpec{{Type: "function", Function: ai.ToolFunction{
		Name: t.tool.Name(), Description: t.tool.Description(), Parameters: t.tool.Parameters(),
	}}}
}

type truncLLM struct{ calls int }

func (l *truncLLM) MaxTokens() int { return 4096 }

func (l *truncLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{ToolCalls: []ai.ToolCall{{
			ID:       "call-1",
			Type:     "function",
			Function: ai.ToolCallFunction{Name: "echo", Arguments: `{}`},
		}}, FinishReason: "tool_calls"}, nil
	}
	return ai.ChatResult{Content: "selesai", FinishReason: "stop"}, nil
}

func runTruncated(t *testing.T, result string, bytes int) string {
	t.Helper()
	llm := &truncLLM{}
	s := scope.New(nil)
	cfg, err := agentloop.ResolveLoopConfig(services.LoopConfig{ToolResultBytes: bytes})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(truncTools{tool: truncTool{result: result}}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, cfg, nil); err != nil {
		t.Fatal(err)
	}
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 1 {
		t.Fatalf("tools = %+v", out.Tools)
	}
	return out.Tools[0].Result
}

func TestToolResultTruncateExactBoundary(t *testing.T) {
	got := runTruncated(t, "abcde", 5)
	if got != "abcde" {
		t.Fatalf("result = %q; want unchanged", got)
	}
}

func TestToolResultTruncateOversized(t *testing.T) {
	got := runTruncated(t, strings.Repeat("x", 20), 5)
	// DSH 4:1 head:tail split of the 5-byte budget: 4 head bytes, 1 tail byte.
	want := "xxxx[... tool result middle pruned ...] (15 bytes omitted)x"
	if got != want {
		t.Fatalf("result = %q; want %q", got, want)
	}
}

func TestToolResultTruncateNeverSplitsMultibyteRune(t *testing.T) {
	got := runTruncated(t, "ééé", 3)
	if strings.Contains(got, "\ufffd") {
		t.Fatalf("result = %q; want no replacement rune", got)
	}
	if !strings.HasPrefix(got, "é") {
		t.Fatalf("result = %q; want valid leading rune", got)
	}
}
