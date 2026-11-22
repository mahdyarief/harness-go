package agentloop_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// hungTool ignores its context, like a tool stuck in a blocking call.
type hungTool struct{ release chan struct{} }

func (hungTool) Name() string        { return "lookup" }
func (hungTool) Description() string { return "hang" }
func (hungTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
}
func (hungTool) ReadOnly() bool { return true }
func (t hungTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	select {
	case <-t.release:
	case <-time.After(3 * time.Second):
	}
	return services.ToolResult{Output: "late"}, nil
}

func TestToolIgnoringContextCannotHoldTheRun(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("h")}},
		{Content: "selesai tanpa alat.", FinishReason: "stop"},
	}}
	s := mountLoop(t, llm, hungTool{release: release}, services.LoopConfig{ToolTimeout: 50 * time.Millisecond})
	start := time.Now()
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("run took %v; a hung tool must not outlive ToolTimeout", elapsed)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 1 || out.Tools[0].Err != "TOOL_TIMEOUT" {
		t.Fatalf("records=%+v; want TOOL_TIMEOUT", out.Tools)
	}
	assertEveryCallAnswered(t, llm.lastHistory)
}
