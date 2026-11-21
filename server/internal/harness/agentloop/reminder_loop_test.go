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

type reminderTool struct{ calls int }

func (t *reminderTool) Name() string        { return "lookup" }
func (t *reminderTool) Description() string { return "lookup" }
func (t *reminderTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (t *reminderTool) ReadOnly() bool { return true }
func (t *reminderTool) Execute(_ context.Context, _ json.RawMessage) (services.ToolResult, error) {
	t.calls++
	return services.ToolResult{Output: "ok"}, nil
}

type reminderLLM struct{ calls int }

func (l *reminderLLM) MaxTokens() int { return 4096 }

func (l *reminderLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{ToolCalls: []ai.ToolCall{
			{ID: "call-1", Type: "function", Function: ai.ToolCallFunction{Name: "lookup", Arguments: `{"q":"a"}`}},
			{ID: "call-2", Type: "function", Function: ai.ToolCallFunction{Name: "lookup", Arguments: `{"q":"b"}`}},
		}, FinishReason: "tool_calls"}, nil
	}
	return ai.ChatResult{Content: "selesai", FinishReason: "stop"}, nil
}

func TestReminderIgnoresDistinctArguments(t *testing.T) {
	tool := &reminderTool{}
	llm := &reminderLLM{}
	s := scope.New(nil)
	cfg := services.LoopConfig{
		ReminderThresholds:   []int{2},
		ReminderPreviewChars: 500,
	}
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(staticTools{tool: tool}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, cfg, nil); err != nil {
		t.Fatal(err)
	}
	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{}, func(e services.AIEvent) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls != 2 {
		t.Fatalf("tool calls = %d; want 2", tool.calls)
	}
	for _, e := range events {
		if strings.Contains(e.Text, "repeating the exact same tool call") {
			t.Fatalf("unexpected reminder for distinct arguments: %+v", events)
		}
	}
	if out.Content != "selesai" {
		t.Fatalf("content = %q", out.Content)
	}
}
