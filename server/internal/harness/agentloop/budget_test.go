package agentloop_test

import (
	"context"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

func TestDispatchAnswersEveryCallAndStopsOnToolBudget(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("a"), call("b"), call("c")}},
		{Content: "tidak boleh tercapai", FinishReason: "stop"},
	}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxToolCalls: 2})
	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")},
		func(e services.AIEvent) error { events = append(events, e); return nil })
	if services.ErrorCode(err) != "TOOL_BUDGET_EXHAUSTED" || !out.Partial {
		t.Fatalf("err=%v partial=%v", err, out.Partial)
	}
	if llm.calls != 1 {
		t.Fatalf("model called %d times after budget", llm.calls)
	}
	assertEveryCallAnswered(t, llm.lastHistory)
	assertHasEvent(t, events, "budget")
	for _, msg := range llm.lastHistory {
		if msg.Role == "tool" && msg.ToolCallID == "c" {
			if msg.Content != "Panggilan alat tidak dijalankan karena batas jumlah panggilan alat tercapai. Selesaikan jawaban dengan informasi yang ada." {
				t.Fatalf("skipped call content=%q", msg.Content)
			}
		}
	}
}
