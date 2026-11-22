package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
	"hsg/internal/lib/ai"
)

type denyTool struct{}

func (denyTool) Name() string                { return "x" }
func (denyTool) Description() string         { return "x" }
func (denyTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (denyTool) ReadOnly() bool              { return true }
func (denyTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{Output: "should not run"}, nil
}

type terminalTool struct{ denyTool }

func (terminalTool) Name() string   { return "submit" }
func (terminalTool) Terminal() bool { return true }

func TestPipelinePreservesTerminalMarker(t *testing.T) {
	s := scope.New(nil)
	wrapped := tools.WrapTools(s, fakeTools{terminalTool{}}, services.LoopConfig{})
	tool, ok := wrapped.Lookup("submit")
	if !ok {
		t.Fatal("submit tool missing")
	}
	terminal, ok := tool.(services.Terminal)
	if !ok || !terminal.Terminal() {
		t.Fatal("terminal marker lost through pipeline wrapping")
	}
	plain, _ := tools.WrapTools(s, fakeTools{denyTool{}}, services.LoopConfig{}).Lookup("x")
	if terminal, ok := plain.(services.Terminal); ok && terminal.Terminal() {
		t.Fatal("non-terminal tool reported terminal")
	}
}

type fakeTools struct{ tool services.Tool }

func (f fakeTools) Lookup(name string) (services.Tool, bool) {
	if f.tool.Name() == name {
		return f.tool, true
	}
	return nil, false
}
func (f fakeTools) Specs() []ai.ToolSpec { return nil }

func TestPipelineCanVetoBeforeExecute(t *testing.T) {
	s := scope.New(nil)
	scope.On(s, "tool.before", func(ev *scope.Event[services.GuardDecision]) error {
		return services.ErrDenied
	})
	wrapped := tools.Pipeline(s, denyTool{})
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if got := services.ErrorCode(err); got != tools.CodeToolDenied {
		t.Fatalf("code=%q; want %s (err=%v)", got, tools.CodeToolDenied, err)
	}
}
