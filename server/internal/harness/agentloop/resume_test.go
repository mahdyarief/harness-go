package agentloop_test

import (
	"context"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type resumeLLM struct {
	calls   int
	script  []ai.ChatResult
	lastErr []error
}

func (l *resumeLLM) MaxTokens() int { return 4096 }

func (l *resumeLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	i := l.calls
	l.calls++
	if i < len(l.script) {
		return l.script[i], l.lastErr[i]
	}
	return ai.ChatResult{Content: "selesai", FinishReason: "stop"}, nil
}

func cut(content string) (ai.ChatResult, error) {
	return ai.ChatResult{Content: content}, &ai.ProviderError{Status: 500, Retryable: false}
}

func TestStreamCutResumesFromCarriedText(t *testing.T) {
	first, firstErr := cut("setengah")
	llm := &resumeLLM{
		script:  []ai.ChatResult{first, {Content: " lagi", FinishReason: "stop"}},
		lastErr: []error{firstErr, nil},
	}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxPartialContinuations: 1})
	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")},
		func(e services.AIEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "setengah lagi" || out.Partial {
		t.Fatalf("content=%q partial=%v; want 'setengah lagi', partial=false", out.Content, out.Partial)
	}
	assertHasEvent(t, events, "notice")
	if got := llm.calls; got != 2 {
		t.Fatalf("calls=%d want 2", got)
	}
}

func TestStreamCutStopsAfterBudgetAndKeepsCarriedText(t *testing.T) {
	first, firstErr := cut("setengah")
	second, secondErr := cut(" dua")
	llm := &resumeLLM{
		script:  []ai.ChatResult{first, second},
		lastErr: []error{firstErr, secondErr},
	}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxPartialContinuations: 1})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err == nil {
		t.Fatal("expected provider error after exhausting partial continuations")
	}
	if out.Content != "setengah dua" || !out.Partial {
		t.Fatalf("content=%q partial=%v; want 'setengah dua', partial=true", out.Content, out.Partial)
	}
	if got := llm.calls; got != 2 {
		t.Fatalf("calls=%d want 2", got)
	}
}

func firstOf(r ai.ChatResult) ai.ChatResult { return r }
func secondOf(_ ai.ChatResult) error {
	return &ai.ProviderError{Status: 500, Retryable: false}
}
