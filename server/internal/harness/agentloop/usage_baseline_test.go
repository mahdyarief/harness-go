package agentloop_test

import (
	"context"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// The provider reports far more prompt tokens than the local 4-chars-per-token
// estimate sees (system prompt, tool schemas, tokenizer differences). DSH's
// meter trusts that usage as the baseline, so compaction must trigger even
// though the local estimate of the history is tiny.
func TestCompactionTriggersOnReportedUsage(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("a")}, PromptTokens: 50000, CompletionTokens: 10},
		{Content: "jawaban", FinishReason: "stop"},
	}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{KeepLastMessages: 1})
	history := []ai.Message{
		{Role: "user", Content: "pertanyaan lama"},
		{Role: "assistant", Content: "jawaban lama"},
		{Role: "user", Content: "pertanyaan baru"},
	}
	var events []services.AIEvent
	_, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: history},
		func(e services.AIEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	assertHasEvent(t, events, "compaction")
	for _, m := range llm.lastHistory {
		if m.Content == "pertanyaan lama" {
			t.Fatal("old turn should have been dropped once reported usage exceeded the cap")
		}
	}
}
