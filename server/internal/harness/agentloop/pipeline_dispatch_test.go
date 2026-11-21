package agentloop_test

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/preset"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
	"hsg/internal/lib/ai"
)

type countingWriteTool struct{ runs int }

func (t *countingWriteTool) Name() string        { return "lookup" }
func (t *countingWriteTool) Description() string { return "tulis" }
func (t *countingWriteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
}
func (t *countingWriteTool) ReadOnly() bool { return false }
func (t *countingWriteTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	t.runs++
	return services.ToolResult{Output: "ok"}, nil
}

// A call the pipeline denies never started, so the UI must not see tool_start.
func TestDeniedToolEmitsNoToolStart(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("w")}},
		{Content: "selesai.", FinishReason: "stop"},
	}}
	tool := &countingWriteTool{}
	cfg := services.LoopConfig{}
	s := mountLoop(t, llm, tool, cfg)
	child := s.Child()
	if err := preset.Apply(child, "read-only"); err != nil {
		t.Fatal(err)
	}
	wrapped := tools.WrapTools(child, staticTools{tool: tool}, cfg)
	if err := child.Provide(services.ToolsKey, wrapped, nil); err != nil {
		t.Fatal(err)
	}
	var events []services.AIEvent
	out, err := agentloop.New(child).Run(context.Background(), services.RunRequest{Messages: userMsg("x")},
		func(e services.AIEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Type == "tool_start" {
			t.Fatalf("denied call emitted tool_start: %+v", events)
		}
	}
	if tool.runs != 0 {
		t.Fatal("denied write tool executed")
	}
	if len(out.Tools) != 1 || out.Tools[0].Err != tools.CodeSandboxReadOnly {
		t.Fatalf("records=%+v; want one SANDBOX_READ_ONLY record", out.Tools)
	}
	assertEveryCallAnswered(t, llm.lastHistory)
}
