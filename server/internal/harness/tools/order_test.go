package tools_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

func TestWriteToolRunsUnderWorkspacePreset(t *testing.T) {
	s := mounted(t, "workspace")
	tool := &writeTool{}
	if _, err := tools.Pipeline(s, tool).Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("workspace-write sandbox must allow write tools: %v", err)
	}
	if !tool.ran {
		t.Fatal("write tool did not run")
	}
}

func TestStrictPresetAsksTheApprovalAnswerer(t *testing.T) {
	cases := []struct {
		name   string
		answer services.ApprovalAnswer
		code   string
		ran    bool
	}{
		{"approved", func(context.Context, services.ToolInvocation) (bool, error) { return true, nil }, "", true},
		{"rejected", func(context.Context, services.ToolInvocation) (bool, error) { return false, nil }, tools.CodeApprovalRejected, false},
		{"no answerer", nil, tools.CodeApprovalUnavailable, false},
		{"timed out", func(context.Context, services.ToolInvocation) (bool, error) {
			return false, context.DeadlineExceeded
		}, tools.CodeApprovalUnavailable, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mounted(t, "strict")
			if tc.answer != nil {
				if err := s.Provide(services.ApprovalAnswerKey, tc.answer, nil); err != nil {
					t.Fatal(err)
				}
			}
			tool := &writeTool{}
			_, err := tools.Pipeline(s, tool).Execute(context.Background(), json.RawMessage(`{}`))
			if got := services.ErrorCode(err); got != tc.code {
				t.Fatalf("code=%q want %q", got, tc.code)
			}
			if tool.ran != tc.ran {
				t.Fatalf("ran=%v want %v", tool.ran, tc.ran)
			}
		})
	}
}

func TestApprovalWaitIsBoundedByTimeout(t *testing.T) {
	s := mounted(t, "strict")
	if err := s.Provide(services.ApprovalAnswerKey, func(ctx context.Context, _ services.ToolInvocation) (bool, error) {
		<-ctx.Done()
		return false, ctx.Err()
	}, nil); err != nil {
		t.Fatal(err)
	}
	tool := &writeTool{}
	wrapped := tools.PipelineWithConfig(s, tool, services.LoopConfig{
		ApprovalMode:    "wait",
		ApprovalTimeout: 20 * time.Millisecond,
	})
	started := time.Now()
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if got := services.ErrorCode(err); got != tools.CodeApprovalUnavailable {
		t.Fatalf("code=%q want %s", got, tools.CodeApprovalUnavailable)
	}
	if tool.ran {
		t.Fatal("tool must not run when the approval wait expires")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("wait did not honour its timeout: %s", elapsed)
	}
}
