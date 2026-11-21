package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
)

type denyTool struct{}

func (denyTool) Name() string                { return "x" }
func (denyTool) Description() string         { return "x" }
func (denyTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (denyTool) ReadOnly() bool              { return true }
func (denyTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{Output: "should not run"}, nil
}

func TestPipelineCanVetoBeforeExecute(t *testing.T) {
	s := scope.New(nil)
	scope.On(s, "tool.before", func(ev *scope.Event[services.GuardDecision]) error {
		return services.ErrDenied
	})
	wrapped := tools.Pipeline(s, denyTool{})
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if got := services.ErrorCode(err); got != "tool.before" {
		t.Fatalf("code=%q; want tool.before (err=%v)", got, err)
	}
}
