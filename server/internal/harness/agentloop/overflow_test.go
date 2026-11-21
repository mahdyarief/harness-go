package agentloop_test

import (
	"context"
	"strings"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/llm"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type overflowLLM struct {
	calls   int
	always  bool
	lastLen int
}

func (o *overflowLLM) MaxTokens() int { return 4096 }
func (o *overflowLLM) ChatStream(_ context.Context, history []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	o.calls++
	o.lastLen = len(history)
	if o.always || o.calls == 1 {
		return ai.ChatResult{}, services.NewHarnessError("context window exceeded", string(llm.CodeContextWindow), nil)
	}
	return ai.ChatResult{Content: "pulih", FinishReason: "stop"}, nil
}

func runOverflow(t *testing.T, always bool) (services.Outcome, error, *overflowLLM) {
	t.Helper()
	model := &overflowLLM{always: always}
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(model), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{ContextTokenCap: 4096}, nil); err != nil {
		t.Fatal(err)
	}
	history := []ai.Message{{Role: "user", Content: strings.Repeat("x", 1200)}}
	for i := 0; i < 3; i++ {
		history = append(history,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: string(rune('a' + i))}}},
			ai.Message{Role: "tool", Content: strings.Repeat("z", 1200)},
		)
	}
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: history}, nil)
	return out, err, model
}

func TestOverflowTriggersOneForceCompactAndRetry(t *testing.T) {
	out, err, model := runOverflow(t, false)
	if err != nil || out.Content != "pulih" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if model.calls != 2 || model.lastLen >= 7 {
		t.Fatalf("calls=%d lastLen=%d; expected compaction", model.calls, model.lastLen)
	}
}

func TestOverflowReturnsErrorAfterOneRetry(t *testing.T) {
	_, err, model := runOverflow(t, true)
	if err == nil || services.ErrorCode(err) != string(llm.CodeContextWindow) {
		t.Fatalf("err=%v; want CONTEXT_WINDOW_EXCEEDED", err)
	}
	if model.calls != 2 {
		t.Fatalf("calls=%d; want 2", model.calls)
	}
}
