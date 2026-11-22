package agentloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type enumTool struct{ runs int }

func (t *enumTool) Name() string        { return "lookup" }
func (t *enumTool) Description() string { return "cari" }
func (t *enumTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","enum":["a","b"]}},"required":["q"],"additionalProperties":false}`)
}
func (t *enumTool) ReadOnly() bool { return true }
func (t *enumTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	t.runs++
	return services.ToolResult{Output: "ok"}, nil
}

type badSchemaTool struct{}

func (badSchemaTool) Name() string                { return "lookup" }
func (badSchemaTool) Description() string         { return "rusak" }
func (badSchemaTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"string"}`) }
func (badSchemaTool) ReadOnly() bool              { return true }
func (badSchemaTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{Output: "ok"}, nil
}

func schemaCall(args string) ai.ToolCall {
	return ai.ToolCall{ID: "call-1", Type: "function", Function: ai.ToolCallFunction{Name: "lookup", Arguments: args}}
}

// A rejected call names the offending paths so the model can repair it.
func TestSchemaViolationNamesPaths(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{schemaCall(`{"q":"z","extra":1}`)}},
		{Content: "selesai.", FinishReason: "stop"},
	}}
	tool := &enumTool{}
	s := mountLoop(t, llm, tool, services.LoopConfig{})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tool.runs != 0 {
		t.Fatal("invalid call reached the tool")
	}
	if len(out.Tools) != 1 || out.Tools[0].Err != "TOOL_ARGS_INVALID" {
		t.Fatalf("records=%+v; want TOOL_ARGS_INVALID", out.Tools)
	}
	if !strings.Contains(out.Tools[0].Result, ".q") || !strings.Contains(out.Tools[0].Result, ".extra") {
		t.Fatalf("result=%q; want both violated paths", out.Tools[0].Result)
	}
	assertEveryCallAnswered(t, llm.lastHistory)
}

func TestMalformedToolSchemaIsRejected(t *testing.T) {
	llm := &recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{schemaCall(`{"q":"a"}`)}},
		{Content: "selesai.", FinishReason: "stop"},
	}}
	s := mountLoop(t, llm, badSchemaTool{}, services.LoopConfig{})
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 1 || out.Tools[0].Err != "TOOL_SCHEMA_INVALID" {
		t.Fatalf("records=%+v; want TOOL_SCHEMA_INVALID", out.Tools)
	}
}
