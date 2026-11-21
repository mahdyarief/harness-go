package agentloop_test

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/guard"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type failingTool struct{}

type failingTools struct{ tool services.Tool }

func (t failingTools) Lookup(name string) (services.Tool, bool) {
	if name == t.tool.Name() {
		return t.tool, true
	}
	return nil, false
}
func (t failingTools) Specs() []ai.ToolSpec {
	return []ai.ToolSpec{{Type: "function", Function: ai.ToolFunction{
		Name: t.tool.Name(), Description: t.tool.Description(), Parameters: t.tool.Parameters(),
	}}}
}

func (failingTool) Name() string        { return "fail" }
func (failingTool) Description() string { return "always fails" }
func (failingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
}
func (failingTool) ReadOnly() bool { return true }
func (failingTool) Execute(_ context.Context, _ json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{}, context.DeadlineExceeded
}

type repeatFailLLM struct{ calls int }

func (l *repeatFailLLM) MaxTokens() int { return 4096 }
func (l *repeatFailLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	return ai.ChatResult{
		FinishReason: "tool_calls",
		ToolCalls: []ai.ToolCall{{
			ID: "f", Type: "function",
			Function: ai.ToolCallFunction{Name: "fail", Arguments: `{"q":"x"}`},
		}},
	}, nil
}

func TestRepeatedErrorGuardStopsRunGracefully(t *testing.T) {
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(&repeatFailLLM{}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(failingTools{tool: failingTool{}}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{
		LoopGuards: []services.LoopGuard{guard.NewRepeatedErrorGuard()},
	}, nil); err != nil {
		t.Fatal(err)
	}
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatalf("guard stop must be graceful, got %v", err)
	}
	if out.Content == "" {
		t.Fatal("guard reason must become the final answer")
	}
	if out.ToolCalls == 0 || out.ToolCalls >= 6 {
		t.Fatalf("tool calls = %d; guard should stop before the turn budget", out.ToolCalls)
	}
}

// flakyOnceTool fails on its first call only.
type flakyOnceTool struct{ calls int }

func (t *flakyOnceTool) Name() string        { return "fail" }
func (t *flakyOnceTool) Description() string { return "fails once" }
func (t *flakyOnceTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
}
func (t *flakyOnceTool) ReadOnly() bool { return true }
func (t *flakyOnceTool) Execute(_ context.Context, _ json.RawMessage) (services.ToolResult, error) {
	t.calls++
	if t.calls == 1 {
		return services.ToolResult{}, context.DeadlineExceeded
	}
	return services.ToolResult{Output: "ok"}, nil
}

// A single failure must not be counted again on later turns.
func TestRepeatedErrorGuardIgnoresEarlierTurnsFailures(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{{ID: "1", Type: "function",
			Function: ai.ToolCallFunction{Name: "fail", Arguments: `{"q":"a"}`}}}},
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{{ID: "2", Type: "function",
			Function: ai.ToolCallFunction{Name: "fail", Arguments: `{"q":"b"}`}}}},
		{Content: "jawaban akhir.", FinishReason: "stop"},
	}}
	s := scope.New(nil)
	_ = s.Provide(services.LLMKey, services.LLM(llm), nil)
	_ = s.Provide(services.ToolsKey, services.Tools(failingTools{tool: &flakyOnceTool{}}), nil)
	_ = s.Provide(services.LoopConfigKey, services.LoopConfig{
		LoopGuards: []services.LoopGuard{guard.NewRepeatedErrorGuard()},
	}, nil)
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "jawaban akhir." {
		t.Fatalf("content=%q; guard stopped on a failure that never repeated", out.Content)
	}
}
