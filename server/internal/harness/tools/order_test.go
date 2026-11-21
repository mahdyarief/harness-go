package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/preset"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
)

type writeTool struct{ ran bool }

func (t *writeTool) Name() string                { return "write" }
func (t *writeTool) Description() string         { return "tulis berkas" }
func (t *writeTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *writeTool) ReadOnly() bool              { return false }
func (t *writeTool) Execute(context.Context, json.RawMessage) (services.ToolResult, error) {
	t.ran = true
	return services.ToolResult{Output: "ok"}, nil
}

func mounted(t *testing.T, presetName string) *scope.Scope {
	t.Helper()
	s := scope.New(nil)
	if err := preset.Apply(s, presetName); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReadOnlyToolRunsUnderStrict(t *testing.T) {
	s := mounted(t, "strict")
	wrapped := tools.Pipeline(s, denyTool{})
	if _, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("read-only tool must run under strict preset: %v", err)
	}
}

func TestWriteToolDeniedUnderReadOnly(t *testing.T) {
	s := mounted(t, "read-only")
	tool := &writeTool{}
	wrapped := tools.Pipeline(s, tool)
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if got := services.ErrorCode(err); got != tools.CodeSandboxReadOnly {
		t.Fatalf("code=%q; want %s", got, tools.CodeSandboxReadOnly)
	}
	if tool.ran {
		t.Fatal("write tool must not execute")
	}
}

func TestToolOutsideAllowlistDenied(t *testing.T) {
	s := scope.New(nil)
	if err := s.Provide(services.ToolAllowlistKey, []string{"other"}, nil); err != nil {
		t.Fatal(err)
	}
	wrapped := tools.Pipeline(s, denyTool{})
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if got := services.ErrorCode(err); got != tools.CodeToolNotAllowed {
		t.Fatalf("code=%q; want %s", got, tools.CodeToolNotAllowed)
	}
}

func TestMountDefaultsToReadOnlyAndNever(t *testing.T) {
	s := mounted(t, "read-only")
	if mode, _ := scope.Require(s, services.SandboxKey); mode != "read-only" {
		t.Fatalf("sandbox=%q; want read-only", mode)
	}
	if policy, _ := scope.Require(s, services.ApprovalKey); policy != "never" {
		t.Fatalf("approval=%q; want never", policy)
	}
}
