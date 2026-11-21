package agentloop_test

import (
	"context"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type recordingLLM struct {
	calls       int
	script      []ai.ChatResult
	lastHistory []ai.Message
	lastOpts    []ai.ChatOptions
}

func (l *recordingLLM) MaxTokens() int { return 4096 }

func (l *recordingLLM) ChatStream(_ context.Context, history []ai.Message, opts ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.lastHistory = append([]ai.Message(nil), history...)
	l.lastOpts = append(l.lastOpts, opts)
	index := l.calls
	l.calls++
	if index < len(l.script) {
		return l.script[index], nil
	}
	return ai.ChatResult{Content: "selesai", FinishReason: "stop"}, nil
}

type blockingLLM struct{}

func (blockingLLM) MaxTokens() int { return 4096 }
func (blockingLLM) ChatStream(ctx context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	<-ctx.Done()
	return ai.ChatResult{}, ctx.Err()
}

func call(id string) ai.ToolCall {
	return ai.ToolCall{
		ID:       id,
		Type:     "function",
		Function: ai.ToolCallFunction{Name: "lookup", Arguments: `{"q":"x"}`},
	}
}

func userMsg(text string) []ai.Message {
	return []ai.Message{{Role: "user", Content: text}}
}

func mountLoop(t *testing.T, llm services.LLM, tool services.Tool, cfg services.LoopConfig) *scope.Scope {
	t.Helper()
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, llm, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(staticTools{tool: tool}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, cfg, nil); err != nil {
		t.Fatal(err)
	}
	return s
}

func assertEveryCallAnswered(t *testing.T, history []ai.Message) {
	t.Helper()
	want := map[string]int{}
	got := map[string]int{}
	for _, msg := range history {
		if msg.Role == "assistant" {
			for _, c := range msg.ToolCalls {
				want[c.ID]++
			}
		}
		if msg.Role == "tool" {
			got[msg.ToolCallID]++
		}
	}
	if len(want) != len(got) {
		t.Fatalf("assistant calls %v != tool answers %v", want, got)
	}
	for id, count := range want {
		if got[id] != count {
			t.Fatalf("call %s answered %d times; want %d", id, got[id], count)
		}
	}
}

func assertHasEvent(t *testing.T, events []services.AIEvent, kind string) {
	t.Helper()
	for _, ev := range events {
		if ev.Type == kind {
			return
		}
	}
	t.Fatalf("no %q event in %+v", kind, events)
}
