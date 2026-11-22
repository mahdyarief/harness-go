package agentloop_test

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/structured"
	"hsg/internal/lib/ai"
)

const submitSchema = `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`

type testTools []services.Tool

func (ts testTools) Lookup(name string) (services.Tool, bool) {
	for _, tool := range ts {
		if tool.Name() == name {
			return tool, true
		}
	}
	return nil, false
}

func (ts testTools) Specs() []ai.ToolSpec {
	var specs []ai.ToolSpec
	for _, tool := range ts {
		specs = append(specs, ai.ToolSpec{Type: "function", Function: ai.ToolFunction{Name: tool.Name(), Description: tool.Description(), Parameters: tool.Parameters()}})
	}
	return specs
}

func mountStructuredLoop(t *testing.T, llm services.LLM, cfg services.LoopConfig, tools ...services.Tool) *scope.Scope {
	t.Helper()
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, llm, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(testTools(tools)), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, cfg, nil); err != nil {
		t.Fatal(err)
	}
	return s
}

func submitCall(id, args string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{Name: "submit", Arguments: args}}
}

func TestStructuredSubmitEndsLoopAndAnswersBatch(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{
		submitCall("submit-1", `{"value":"done"}`),
		schemaCall(`{"q":"a"}`),
	}}}}
	sibling := &enumTool{}
	tools := []services.Tool{structured.Submit("submit", "submit result", json.RawMessage(submitSchema)), sibling}
	s := mountStructuredLoop(t, llm, services.LoopConfig{RequireStructured: "submit"}, tools...)
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("work")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Turns != 1 || string(out.Structured) != `{"value":"done"}` {
		t.Fatalf("Turns=%d Structured=%s; want one turn with validated JSON", out.Turns, out.Structured)
	}
	if llm.calls != 1 {
		t.Fatalf("LLM calls=%d; want no turn after the terminal submit", llm.calls)
	}
	if sibling.runs != 1 {
		t.Fatalf("same-batch tool runs=%d; want 1", sibling.runs)
	}
}

func TestSuccessfulLaterTerminalCallEndsLoop(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{
		submitCall("submit-invalid", `{"value":3}`),
		submitCall("submit-valid", `{"value":"valid"}`),
	}}}}
	s := mountStructuredLoop(t, llm, services.LoopConfig{}, structured.Submit("submit", "submit result", json.RawMessage(submitSchema)))
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("work")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Turns != 1 || string(out.Structured) != `{"value":"valid"}` || llm.calls != 1 {
		t.Fatalf("Turns=%d Structured=%s LLM calls=%d; want later successful terminal result and no extra turn", out.Turns, out.Structured, llm.calls)
	}
}

func TestOnlyArgumentErrorsConsumeStructuredRepairs(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{submitCall("submit-1", `{"value":"ok"}`)}},
		{Content: "complete", FinishReason: "stop"},
	}}
	tool := structured.Submit("submit", "submit result", json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`))
	s := mountStructuredLoop(t, llm, services.LoopConfig{RequireStructured: "submit", MaxStructuredRepairs: 1}, tool)
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("work")}, nil)
	if services.ErrorCode(err) != "STRUCTURED_MISSING" {
		t.Fatalf("err=%v; want non-argument tool error not to consume repair", err)
	}
	if out.Turns != 2 {
		t.Fatalf("Turns=%d; want one nudge after tool execution failure", out.Turns)
	}
}

func TestStructuredInvalidCallIsRepaired(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{submitCall("submit-1", `{"value":3}`)}},
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{submitCall("submit-2", `{"value":"fixed"}`)}},
	}}
	s := mountStructuredLoop(t, llm, services.LoopConfig{RequireStructured: "submit"}, structured.Submit("submit", "submit result", json.RawMessage(submitSchema)))
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("work")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Turns != 2 || string(out.Structured) != `{"value":"fixed"}` {
		t.Fatalf("Turns=%d Structured=%s; want repaired result on turn two", out.Turns, out.Structured)
	}
}

func TestStructuredMissingErrorsAfterOneNudge(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{{Content: "not yet", FinishReason: "stop"}, {Content: "still no submit", FinishReason: "stop"}}}
	s := mountStructuredLoop(t, llm, services.LoopConfig{RequireStructured: "submit"}, structured.Submit("submit", "submit result", json.RawMessage(submitSchema)))
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("work")}, nil)
	if services.ErrorCode(err) != "STRUCTURED_MISSING" {
		t.Fatalf("err=%v; want STRUCTURED_MISSING", err)
	}
	if out.Turns != 2 {
		t.Fatalf("Turns=%d; want exactly one nudge then failure", out.Turns)
	}
}

func TestStructuredInvalidExhaustsRepairs(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{submitCall("submit-1", `{"value":3}`)}},
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{submitCall("submit-2", `{"value":false}`)}},
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{submitCall("submit-3", `{"value":3}`)}},
	}}
	s := mountStructuredLoop(t, llm, services.LoopConfig{RequireStructured: "submit"}, structured.Submit("submit", "submit result", json.RawMessage(submitSchema)))
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("work")}, nil)
	if services.ErrorCode(err) != "STRUCTURED_INVALID" {
		t.Fatalf("err=%v; want STRUCTURED_INVALID", err)
	}
	if out.Turns != 3 {
		t.Fatalf("Turns=%d; want three invalid attempts with two repair chances", out.Turns)
	}
}

func TestWithoutStructuredRequirementKeepsCurrentBehavior(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{{Content: "all done.", FinishReason: "stop"}}}
	s := mountStructuredLoop(t, llm, services.LoopConfig{}, structured.Submit("submit", "submit result", json.RawMessage(submitSchema)))
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("work")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "all done." || out.Turns != 1 || len(out.Structured) != 0 {
		t.Fatalf("out=%+v; want unchanged text completion", out)
	}
}

func TestLoopConfigStructuredRepairDefault(t *testing.T) {
	cfg, err := agentloop.ResolveLoopConfig(services.LoopConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxStructuredRepairs != 2 {
		t.Fatalf("MaxStructuredRepairs=%d; want 2", cfg.MaxStructuredRepairs)
	}
}
