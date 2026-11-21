package agentloop_test

import (
	"context"
	"testing"

	"hsg/internal/harness/agentloop"
	hsgllm "hsg/internal/harness/llm"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// gatewayCutLLM behaves like the real gateway: text reaches the client through
// onDelta, but a truncated stream returns usage only and ErrOutput (wrapped by
// the LLM adapter as STREAM_CLOSED).
type gatewayCutLLM struct{ calls int }

func (l *gatewayCutLLM) MaxTokens() int { return 4096 }
func (l *gatewayCutLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		_ = onDelta(ai.StreamDelta{Content: "setengah"})
		return ai.ChatResult{PromptTokens: 5, CompletionTokens: 3},
			hsgllm.NewFailure("provider request failed", string(hsgllm.CodeStreamClosed), ai.ErrOutput)
	}
	_ = onDelta(ai.StreamDelta{Content: " lagi"})
	return ai.ChatResult{Content: " lagi", FinishReason: "stop", PromptTokens: 7, CompletionTokens: 2}, nil
}

func TestRealStreamCutResumesFromStreamedText(t *testing.T) {
	llm := &gatewayCutLLM{}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxPartialContinuations: 1})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "setengah lagi" || llm.calls != 2 {
		t.Fatalf("content=%q calls=%d; want resumed answer", out.Content, llm.calls)
	}
	if out.PromptTokens != 12 || out.CompletionTokens != 5 {
		t.Fatalf("usage=%d/%d; want both attempts billed", out.PromptTokens, out.CompletionTokens)
	}
}

// retryUsageLLM fails once with a retryable code before any text is streamed.
type retryUsageLLM struct{ calls int }

func (l *retryUsageLLM) MaxTokens() int { return 4096 }
func (l *retryUsageLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{PromptTokens: 5}, services.NewHarnessError("server", string(hsgllm.CodeServer), nil)
	}
	return ai.ChatResult{Content: "ok", FinishReason: "stop", PromptTokens: 7, CompletionTokens: 2}, nil
}

func TestRetryAttemptsAreAllBilled(t *testing.T) {
	llm := &retryUsageLLM{}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxRetries: 1})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil || out.Content != "ok" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if out.PromptTokens != 12 {
		t.Fatalf("prompt tokens=%d; want 12 (failed attempt included)", out.PromptTokens)
	}
}
