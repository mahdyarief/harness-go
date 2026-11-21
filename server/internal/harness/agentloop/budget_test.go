package agentloop_test

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestMaxDurationAbortsRun(t *testing.T) {
	s := mountLoop(t, blockingLLM{}, &fakeTool{}, services.LoopConfig{MaxDuration: 50 * time.Millisecond})
	start := time.Now()
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !out.Partial {
		t.Fatalf("err=%v partial=%v", err, out.Partial)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("elapsed=%v; expected prompt abort", elapsed)
	}
}

func TestTokenBudgetStopsBeforeSecondCall(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("a")}, CompletionTokens: 11},
		{Content: "tidak boleh tercapai", FinishReason: "stop"},
	}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxTotalTokens: 10})
	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")},
		func(e services.AIEvent) error { events = append(events, e); return nil })
	if services.ErrorCode(err) != "TOKEN_BUDGET_EXHAUSTED" || !out.Partial {
		t.Fatalf("err=%v partial=%v", err, out.Partial)
	}
	if llm.calls != 1 {
		t.Fatalf("model called %d times", llm.calls)
	}
	assertHasEvent(t, events, "budget")
}

func TestMaxTokensPassedToLLMIsRemainingBudget(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("a")}, CompletionTokens: 3},
		{Content: "jawaban", FinishReason: "stop"},
	}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxTotalTokens: 20})
	if _, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil); err != nil {
		t.Fatal(err)
	}
	if len(llm.lastOpts) < 2 {
		t.Fatalf("opts=%+v", llm.lastOpts)
	}
	if llm.lastOpts[0].MaxTokens != 20 {
		t.Errorf("first MaxTokens=%d want 20", llm.lastOpts[0].MaxTokens)
	}
	if llm.lastOpts[1].MaxTokens != 17 {
		t.Errorf("second MaxTokens=%d want 17", llm.lastOpts[1].MaxTokens)
	}
}

func TestLengthFinishReasonIsStickyAcrossToolTurn(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "length", ToolCalls: []ai.ToolCall{call("a")}},
		{Content: "jawaban", FinishReason: "stop"},
	}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Partial {
		t.Fatal("length finish reason must set Partial")
	}
}

// Prompt tokens are re-sent every turn, so counting them against the budget
// would end ordinary multi-tool runs; only completion tokens are budgeted.
func TestTokenBudgetIgnoresPromptTokens(t *testing.T) {
	var script []ai.ChatResult
	for i := 0; i < 4; i++ {
		script = append(script, ai.ChatResult{
			FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call(string(rune('a' + i)))},
			PromptTokens: 9000, CompletionTokens: 10,
		})
	}
	script = append(script, ai.ChatResult{Content: "jawaban", FinishReason: "stop", PromptTokens: 9000, CompletionTokens: 10})
	llm := &recordingLLM{script: script}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxTotalTokens: 20000, MaxTurns: 8, MaxToolCalls: 8})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatalf("err=%v; prompt tokens must not exhaust the budget", err)
	}
	if out.Content != "jawaban" {
		t.Fatalf("content=%q", out.Content)
	}
	if last := llm.lastOpts[len(llm.lastOpts)-1].MaxTokens; last != 20000-40 {
		t.Fatalf("last MaxTokens=%d want %d", last, 20000-40)
	}
}
