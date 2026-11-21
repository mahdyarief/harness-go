package agentloop_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type compactLLM struct{ calls int }

func (l *compactLLM) MaxTokens() int { return 4096 }
func (l *compactLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{Content: strings.Repeat("x", 400), FinishReason: "stop"}, nil
	}
	return ai.ChatResult{Content: "selesai", FinishReason: "stop"}, nil
}

func TestCompactionPrunesMiddleAndEmitsEvent(t *testing.T) {
	s := scope.New(nil)
	llm := &compactLLM{}
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := agentloop.ResolveLoopConfig(services.LoopConfig{ContextTokenCap: 1, KeepLastMessages: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, cfg, nil); err != nil {
		t.Fatal(err)
	}
	history := []ai.Message{{Role: "user", Content: strings.Repeat("y", 400)}}
	for i := 0; i < 4; i++ {
		history = append(history,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: fmt.Sprintf("c%d", i)}}},
			ai.Message{Role: "tool", Content: strings.Repeat("z", 9000)}, // above the DSH 8192 prune threshold
		)
	}
	var events []services.AIEvent
	_, err = agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: history},
		func(e services.AIEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range events {
		if e.Type == "compaction" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no compaction event in %+v", events)
	}
}
