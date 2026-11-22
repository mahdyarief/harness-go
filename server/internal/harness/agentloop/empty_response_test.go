package agentloop_test

import (
	"context"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// DSH treats a completed stream with no text and no tool calls as
// EMPTY_RESPONSE, a default-retryable failure.
func TestEmptyResponseIsRetried(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "stop", PromptTokens: 4},
		{Content: "jawaban.", FinishReason: "stop", PromptTokens: 4, CompletionTokens: 2},
	}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxRetries: 1})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil || out.Content != "jawaban." || llm.calls != 2 {
		t.Fatalf("out=%q err=%v calls=%d; want retried answer", out.Content, err, llm.calls)
	}
	if out.PromptTokens != 8 {
		t.Fatalf("prompt tokens=%d; the empty attempt must be billed", out.PromptTokens)
	}
}

// Once retries are spent the empty result goes to the continuation policy
// unchanged, as before, instead of failing the run.
func TestEmptyResponseAfterRetriesKeepsLegacyBehaviour(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{{FinishReason: "stop"}, {FinishReason: "stop"}}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxRetries: 1})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatalf("err=%v; exhausted empty retries must not fail the run", err)
	}
	if llm.calls != 2 || out.Content != "" {
		t.Fatalf("calls=%d content=%q", llm.calls, out.Content)
	}
}
