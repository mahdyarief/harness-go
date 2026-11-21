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

const (
	CodeSandboxReadOnly     = "SANDBOX_READ_ONLY"
	CodeApprovalRejected    = "APPROVAL_REJECTED"
	CodeApprovalUnavailable = "APPROVAL_UNAVAILABLE"
	CodeToolNotAllowed      = "TOOL_NOT_ALLOWED"
)

type guardedTool struct {
	scope  *scope.Scope
	next   services.Tool
	cfg    services.LoopConfig
	answer func(services.ToolInvocation) (bool, error)
}

func Pipeline(s *scope.Scope, next services.Tool) services.Tool {
	return PipelineWithConfig(s, next, services.LoopConfig{ToolTimeout: defaultToolTimeout})
}

func PipelineWithConfig(s *scope.Scope, next services.Tool, cfg services.LoopConfig) services.Tool {
	timeout := cfg.ToolTimeout
	if timeout <= 0 {
		timeout = defaultToolTimeout
	}
	cfg.ToolTimeout = timeout
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

// SetApprovalAnswer wires an interactive answerer for "ask" approval mode.
func (g *guardedTool) SetApprovalAnswer(fn func(services.ToolInvocation) (bool, error)) {
	g.answer = fn
}

// pre runs the ordered links. A deny short-circuits the chain; otherwise each
// link may pass its decision to the next via next().
func (g *guardedTool) pre(ctx context.Context, call services.ToolInvocation) services.PreDecision {
	decision := services.PreAllow()
	links := []services.PreExecute{g.presetLink, g.sandboxLink, g.approvalLink, g.listenerLink}
	for _, link := range links {
		decision = link(ctx, call, func() services.PreDecision { return decision })
		if decision.Kind == "deny" {
			return decision
		}
	}
	return decision
}

func (g *guardedTool) presetLink(_ context.Context, call services.ToolInvocation, next func() services.PreDecision) services.PreDecision {
	allowlist, ok := scope.Require(g.scope, services.ToolAllowlistKey)
	if ok && len(allowlist) > 0 && !contains(allowlist, call.Name) {
		return services.PreDeny("alat "+call.Name+" tidak diizinkan oleh preset", CodeToolNotAllowed)
	}
	return next()
}

func (g *guardedTool) sandboxLink(_ context.Context, call services.ToolInvocation, next func() services.PreDecision) services.PreDecision {
	mode, _ := scope.Require(g.scope, services.SandboxKey)
	if mode == "read-only" && !call.ReadOnly {
		return services.PreDeny("alat "+call.Name+" menulis tetapi sandbox read-only", CodeSandboxReadOnly)
	}
	return next()
}

func (g *guardedTool) approvalLink(_ context.Context, call services.ToolInvocation, next func() services.PreDecision) services.PreDecision {
	// Only write tools ask for approval; read-only tools never do.
	if call.ReadOnly {
		return next()
	}
	policy, _ := scope.Require(g.scope, services.ApprovalKey)
	switch policy {
	case "ask":
		if g.answer == nil {
			return services.PreDeny("alat "+call.Name+" memerlukan persetujuan tetapi tidak ada penjawab", CodeApprovalUnavailable)
		}
		ok, err := g.answer(call)
		if err != nil || !ok {
			return services.PreDeny("alat "+call.Name+" ditolak oleh pengguna", CodeApprovalRejected)
		}
		return next()
	case "", "never":
		return services.PreDeny("alat "+call.Name+" memerlukan persetujuan tetapi kebijakan never", CodeApprovalRejected)
	default:
		return next()
	}
}

func (g *guardedTool) listenerLink(_ context.Context, _ services.ToolInvocation, next func() services.PreDecision) services.PreDecision {
	decision := services.GuardDecision{Allowed: true}
	out, err := scope.Waterfall(g.scope, scope.Event[services.GuardDecision]{Name: "tool.before", Payload: decision}, decision)
	if err != nil || !out.Allowed {
		reason := out.Reason
		if reason == "" {
			reason = "alat ditolak"
		}
		return services.PreDeny(reason, "tool.before")
	}
	return next()
}

func (g *guardedTool) Execute(ctx context.Context, args json.RawMessage) (services.ToolResult, error) {
	call := services.ToolInvocation{Name: g.next.Name(), Arguments: args, ReadOnly: g.next.ReadOnly()}
	decision := g.pre(ctx, call)
	if decision.Kind != "allow" {
		message := decision.Reason
		if message == "" {
			message = "alat tidak diizinkan"
		}
		_ = scope.Emit(g.scope, scope.Event[services.GuardDecision]{Name: "tool.after", Payload: services.GuardDecision{Reason: decision.Code}})
		return services.ToolResult{Output: message}, services.NewHarnessError(message, decision.Code, nil)
	}

	var result services.ToolResult
	err := guard.RunWithTimeout(ctx, g.cfg.ToolTimeout, func(ctx context.Context) error {
		var e error
		result, e = g.next.Execute(ctx, args)
		return e
	})
	if err != nil {
		return services.ToolResult{}, err
	}
	_ = scope.Emit(g.scope, scope.Event[services.GuardDecision]{Name: "tool.after", Payload: services.GuardDecision{Allowed: true}})
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
