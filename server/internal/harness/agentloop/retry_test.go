package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/llm"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type retryLLM struct {
	calls   int
	delay   bool
	code    string
	onDelta bool
}

func (r *retryLLM) MaxTokens() int { return 4096 }
func (r *retryLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	r.calls++
	if r.onDelta {
		_ = onDelta(ai.StreamDelta{Content: "partial"})
	}
	if r.calls == 1 {
		return ai.ChatResult{}, services.NewHarnessError("rate limited", r.code, nil)
	}
	return ai.ChatResult{Content: "ok", FinishReason: "stop"}, nil
}

func TestCallRetriesOnRateLimit(t *testing.T) {
	r := &retryLLM{code: string(llm.CodeRateLimit)}
	s := mountLoop(t, r, &fakeTool{}, services.LoopConfig{MaxRetries: 1})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil || out.Content != "ok" || r.calls != 2 {
		t.Fatalf("out=%+v err=%v calls=%d", out, err, r.calls)
	}
}

func TestCallDoesNotRetryAuthOrAfterDelta(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  string
		delta bool
	}{
		{"auth", string(llm.CodeAuth), false},
		{"delta", string(llm.CodeRateLimit), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &retryLLM{code: tc.code, onDelta: tc.delta}
			s := mountLoop(t, r, &fakeTool{}, services.LoopConfig{MaxRetries: 3})
			_, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if r.calls != 1 {
				t.Fatalf("calls=%d; retry must not happen", r.calls)
			}
			var he *services.HarnessError
			if !errors.As(err, &he) && !errors.Is(err, ai.ErrProvider) {
				t.Fatalf("err=%v; want a classified error", err)
			}
		})
	}
}
