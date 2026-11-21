package agentloop_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
	"hsg/internal/lib/ai"
)

type scriptedLLM struct{ calls int }

func (l *scriptedLLM) MaxTokens() int { return 4096 }

func (l *scriptedLLM) ChatStream(_ context.Context, _ []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		if len(opts.Tools) == 0 {
			return ai.ChatResult{}, ai.ErrOutput
		}
		return ai.ChatResult{
			ToolCalls: []ai.ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: ai.ToolCallFunction{Name: "lookup", Arguments: `{"q":"x"}`},
			}},
			FinishReason:     "tool_calls",
			PromptTokens:     7,
			CompletionTokens: 2,
		}, nil
	}
	_ = onDelta(ai.StreamDelta{Content: "jawaban"})
	return ai.ChatResult{
		Content:          "jawaban",
		FinishReason:     "stop",
		PromptTokens:     9,
		CompletionTokens: 3,
	}, nil
}

type fakeTool struct{ executed bool }

func (t *fakeTool) Name() string        { return "lookup" }
func (t *fakeTool) Description() string { return "cari data" }
func (t *fakeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (t *fakeTool) ReadOnly() bool { return true }
func (t *fakeTool) Execute(_ context.Context, args json.RawMessage) (services.ToolResult, error) {
	t.executed = true
	var in struct {
		Q string `json:"q"`
	}
	_ = json.Unmarshal(args, &in)
	return services.ToolResult{Output: "hasil:" + in.Q}, nil
}

type staticTools struct{ tool services.Tool }

func (s staticTools) Lookup(name string) (services.Tool, bool) {
	if name == s.tool.Name() {
		return s.tool, true
	}
	return nil, false
}
func (s staticTools) Specs() []ai.ToolSpec {
	return []ai.ToolSpec{{Type: "function", Function: ai.ToolFunction{
		Name: s.tool.Name(), Description: s.tool.Description(), Parameters: s.tool.Parameters(),
	}}}
}

func TestAgentLoopExecutesToolThenAnswers(t *testing.T) {
	tool := &fakeTool{}
	llm := &scriptedLLM{}
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(staticTools{tool: tool}), nil); err != nil {
		t.Fatal(err)
	}

	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{
		Messages: []ai.Message{{Role: "user", Content: "cari x"}},
	}, func(ev services.AIEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tool.executed {
		t.Fatal("tool was not executed")
	}
	if out.Content != "jawaban" || out.ToolCalls != 1 || out.Turns != 2 {
		t.Fatalf("outcome mismatch: %+v", out)
	}
	if out.PromptTokens != 16 || out.CompletionTokens != 5 {
		t.Fatalf("tokens = %d/%d; want 16/5", out.PromptTokens, out.CompletionTokens)
	}
	if len(out.Tools) != 1 || out.Tools[0].Name != "lookup" || out.Tools[0].Result != "hasil:x" {
		t.Fatalf("tool records = %+v", out.Tools)
	}
	if len(events) == 0 || events[0].Type != "tool_start" {
		t.Fatalf("events = %+v", events)
	}
}

type denyGuardLLM struct{ calls int }

func (l *denyGuardLLM) MaxTokens() int { return 4096 }
func (l *denyGuardLLM) ChatStream(_ context.Context, _ []ai.Message, opts ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{
			ToolCalls: []ai.ToolCall{{
				ID:       "deny-1",
				Type:     "function",
				Function: ai.ToolCallFunction{Name: "lookup", Arguments: `{"q":"x"}`},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	return ai.ChatResult{Content: "selesai", FinishReason: "stop"}, nil
}

func TestToolBeforeGuardCanDenyInvocation(t *testing.T) {
	tool := &fakeTool{}
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(&denyGuardLLM{}), nil); err != nil {
		t.Fatal(err)
	}
	wrapped := tools.WrapTools(s, staticTools{tool: tool}, services.LoopConfig{})
	if err := s.Provide(services.ToolsKey, wrapped, nil); err != nil {
		t.Fatal(err)
	}
	scope.On(s, "tool.before", func(ev *scope.Event[services.GuardDecision]) error {
		ev.Payload = services.GuardDecision{Allowed: false, Reason: "ditolak"}
		return nil
	})

	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{
		Messages: []ai.Message{{Role: "user", Content: "cari x"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool.executed {
		t.Fatal("tool ran despite denial")
	}
	if len(out.Tools) != 1 || out.Tools[0].Err != tools.CodeToolDenied {
		t.Fatalf("records = %+v", out.Tools)
	}
}

type timeoutLLM struct{ calls int }

func (l *timeoutLLM) MaxTokens() int { return 4096 }
func (l *timeoutLLM) ChatStream(_ context.Context, _ []ai.Message, opts ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{
			ToolCalls: []ai.ToolCall{{
				ID:       "timeout-1",
				Type:     "function",
				Function: ai.ToolCallFunction{Name: "slow", Arguments: `{"q":"x"}`},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	return ai.ChatResult{Content: "selesai", FinishReason: "stop"}, nil
}

type slowTool struct{}

func (slowTool) Name() string        { return "slow" }
func (slowTool) Description() string { return "tool lambat" }
func (slowTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (slowTool) ReadOnly() bool { return true }
func (slowTool) Execute(ctx context.Context, _ json.RawMessage) (services.ToolResult, error) {
	<-ctx.Done()
	return services.ToolResult{}, ctx.Err()
}

func TestTimedOutToolRecordsStableCodeAndModelFacingMessage(t *testing.T) {
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(&timeoutLLM{}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(staticTools{tool: slowTool{}}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{ToolTimeout: 10 * time.Millisecond}, nil); err != nil {
		t.Fatal(err)
	}

	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{
		Messages: []ai.Message{{Role: "user", Content: "cari x"}},
	}, func(ev services.AIEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 1 {
		t.Fatalf("records = %+v", out.Tools)
	}
	if out.Tools[0].Err != "TOOL_TIMEOUT" {
		t.Errorf("record code = %q; want TOOL_TIMEOUT", out.Tools[0].Err)
	}
	if out.Tools[0].Result != "tool timed out" {
		t.Errorf("model-facing result = %q; want 'tool timed out'", out.Tools[0].Result)
	}
}

type continuationLLM struct{ calls int }

func (l *continuationLLM) MaxTokens() int { return 4096 }
func (l *continuationLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	switch l.calls {
	case 1:
		return ai.ChatResult{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("a")}}, nil
	case 2:
		return ai.ChatResult{Content: "Saya akan cek datanya", FinishReason: "stop"}, nil
	default:
		return ai.ChatResult{Content: "hasil akhir", FinishReason: "stop"}, nil
	}
}

func TestAntiStallContinuationRunsAnotherTurn(t *testing.T) {
	llm := &continuationLLM{}
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(staticTools{tool: &fakeTool{}}), nil); err != nil {
		t.Fatal(err)
	}
	var events []services.AIEvent
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{
		Messages: []ai.Message{{Role: "user", Content: "tolong cek"}},
	}, func(e services.AIEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 3 || out.Turns != 3 || out.Content != "hasil akhir" {
		t.Fatalf("calls=%d out=%+v", llm.calls, out)
	}
	var notices int
	for _, e := range events {
		if e.Type == "notice" {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("notices=%d want 1", notices)
	}
}

func TestNoToolsNoNudge(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{{Content: "Saya akan jelaskan: ...", FinishReason: "stop"}}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 1 || out.Turns != 1 {
		t.Fatalf("stalled answer without tools must not be nudged: calls=%d turns=%d", llm.calls, out.Turns)
	}
}

func TestContinuationNudgeCap(t *testing.T) {
	stall := ai.ChatResult{Content: "Saya akan cek datanya", FinishReason: "stop"}
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{call("a")}},
		stall, stall, stall, stall,
	}}
	s := mountLoop(t, llm, &fakeTool{}, services.LoopConfig{MaxContinuationNudges: 1})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 3 {
		t.Fatalf("calls=%d want 3 (one tool turn, one nudge, then stop)", llm.calls)
	}
	if !out.Partial {
		t.Fatal("capped nudge must leave the turn partial")
	}
}
