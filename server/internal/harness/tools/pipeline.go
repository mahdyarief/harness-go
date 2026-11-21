package tools

import (
	"context"
	"encoding/json"
	"time"

	"hsg/internal/harness/guard"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

const defaultToolTimeout = 30 * time.Second

// Stable denial codes. Approval codes are DSH core/tools/src/error-codes.ts:12-18.
const (
	CodeSandboxReadOnly     = "SANDBOX_READ_ONLY"
	CodeApprovalRejected    = "APPROVAL_REJECTED"
	CodeApprovalUnavailable = "APPROVAL_UNAVAILABLE"
	CodeToolNotAllowed      = "TOOL_NOT_ALLOWED"
	CodeToolDenied          = "TOOL_DENIED"
)

type guardedTool struct {
	scope *scope.Scope
	next  services.Tool
	cfg   services.LoopConfig
}

func Pipeline(s *scope.Scope, next services.Tool) services.Tool {
	return PipelineWithConfig(s, next, services.LoopConfig{ToolTimeout: defaultToolTimeout})
}

func PipelineWithConfig(s *scope.Scope, next services.Tool, cfg services.LoopConfig) services.Tool {
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = defaultToolTimeout
	}
	return &guardedTool{scope: s, next: next, cfg: cfg}
}

type wrappedTools struct {
	inner services.Tools
	wrap  func(services.Tool) services.Tool
}

func (w wrappedTools) Lookup(name string) (services.Tool, bool) {
	tool, ok := w.inner.Lookup(name)
	if !ok {
		return nil, false
	}
	return w.wrap(tool), true
}
func (w wrappedTools) Specs() []ai.ToolSpec { return w.inner.Specs() }

// WrapTools applies the pre-execute pipeline to every tool in a registry.
func WrapTools(s *scope.Scope, inner services.Tools, cfg services.LoopConfig) services.Tools {
	return wrappedTools{inner: inner, wrap: func(tool services.Tool) services.Tool {
		return PipelineWithConfig(s, tool, cfg)
	}}
}

func (g *guardedTool) Name() string                { return g.next.Name() }
func (g *guardedTool) Description() string         { return g.next.Description() }
func (g *guardedTool) Parameters() json.RawMessage { return g.next.Parameters() }
func (g *guardedTool) ReadOnly() bool              { return g.next.ReadOnly() }

// pre runs the ordered checks of DSH core/tools/src/pipeline.ts:191 (prepare):
// preset allow-list, sandbox, approval of any escalation the sandbox asked
// for, then user tool.before listeners. The first denial wins.
func (g *guardedTool) pre(ctx context.Context, call services.ToolInvocation) services.PreDecision {
	allowlist, _ := scope.Require(g.scope, services.ToolAllowlistKey)
	if len(allowlist) > 0 && !contains(allowlist, call.Name) {
		return services.PreDeny("alat "+call.Name+" tidak diizinkan oleh preset", CodeToolNotAllowed)
	}
	if decision := g.sandbox(call); decision.Kind == "ask" {
		if decision = g.approve(ctx, call, decision); decision.Kind != "allow" {
			return decision
		}
	}
	return g.listeners()
}

// sandbox asks for escalation when a write tool meets a read-only sandbox.
// workspace-write (and wider) sandboxes let write tools run without asking.
func (g *guardedTool) sandbox(call services.ToolInvocation) services.PreDecision {
	mode, _ := scope.Require(g.scope, services.SandboxKey)
	if !call.ReadOnly && (mode == "" || mode == "read-only") {
		return services.PreDecision{
			Kind:   "ask",
			Reason: "alat " + call.Name + " menulis tetapi sandbox read-only",
			Code:   CodeSandboxReadOnly,
		}
	}
	return services.PreAllow()
}

// approve resolves an ask. "never" means no escalation is possible; "ask"
// consults the scope's answerer and fails closed without one.
func (g *guardedTool) approve(ctx context.Context, call services.ToolInvocation, ask services.PreDecision) services.PreDecision {
	policy, _ := scope.Require(g.scope, services.ApprovalKey)
	if policy != "ask" {
		return services.PreDeny(ask.Reason, ask.Code)
	}
	answer, ok := scope.Require(g.scope, services.ApprovalAnswerKey)
	if !ok || answer == nil {
		return services.PreDeny("alat "+call.Name+" memerlukan persetujuan tetapi tidak ada penjawab", CodeApprovalUnavailable)
	}
	approved, err := answer(ctx, call)
	if err != nil || !approved {
		return services.PreDeny("alat "+call.Name+" ditolak oleh pengguna", CodeApprovalRejected)
	}
	return services.PreAllow()
}

func (g *guardedTool) listeners() services.PreDecision {
	decision := services.GuardDecision{Allowed: true}
	out, err := scope.Waterfall(g.scope, scope.Event[services.GuardDecision]{Name: "tool.before", Payload: decision}, decision)
	if err != nil || !out.Allowed {
		reason := out.Reason
		if reason == "" {
			reason = "alat ditolak"
		}
		return services.PreDeny(reason, CodeToolDenied)
	}
	return services.PreAllow()
}

// Prepare implements services.Preparer: checks first, execution later.
func (g *guardedTool) Prepare(ctx context.Context, args json.RawMessage) (func(context.Context) (services.ToolResult, error), error) {
	call := services.ToolInvocation{Name: g.next.Name(), Arguments: args, ReadOnly: g.next.ReadOnly()}
	if decision := g.pre(ctx, call); decision.Kind != "allow" {
		_ = scope.Emit(g.scope, scope.Event[services.GuardDecision]{Name: "tool.after", Payload: services.GuardDecision{Reason: decision.Code}})
		return nil, services.NewHarnessError(decision.Reason, decision.Code, nil)
	}
	return func(ctx context.Context) (services.ToolResult, error) {
		result, err := g.next.Execute(ctx, args)
		if err == nil {
			_ = scope.Emit(g.scope, scope.Event[services.GuardDecision]{Name: "tool.after", Payload: services.GuardDecision{Allowed: true}})
		}
		return result, err
	}, nil
}

// Execute is the standalone path (no loop): prepare, then run under the
// pipeline's own deadline. The loop calls Prepare and applies its deadline.
func (g *guardedTool) Execute(ctx context.Context, args json.RawMessage) (services.ToolResult, error) {
	run, err := g.Prepare(ctx, args)
	if err != nil {
		return services.ToolResult{Output: err.Error()}, err
	}
	var result services.ToolResult
	err = guard.RunWithTimeout(ctx, g.cfg.ToolTimeout, func(ctx context.Context) error {
		var e error
		result, e = run(ctx)
		return e
	})
	if err != nil {
		return services.ToolResult{}, err
	}
	return result, nil
}

func contains(list []string, name string) bool {
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}
