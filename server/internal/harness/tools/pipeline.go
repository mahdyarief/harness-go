package tools

import (
	"context"
	"encoding/json"
	"time"

	"hsg/internal/harness/guard"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

const defaultToolTimeout = 30 * time.Second

type guardedTool struct {
	scope *scope.Scope
	next  services.Tool
}

func Pipeline(s *scope.Scope, next services.Tool) services.Tool {
	return &guardedTool{scope: s, next: next}
}

func (g *guardedTool) Name() string                { return g.next.Name() }
func (g *guardedTool) Description() string         { return g.next.Description() }
func (g *guardedTool) Parameters() json.RawMessage { return g.next.Parameters() }
func (g *guardedTool) ReadOnly() bool              { return g.next.ReadOnly() }

func (g *guardedTool) Execute(ctx context.Context, args json.RawMessage) (services.ToolResult, error) {
	decision := services.GuardDecision{Allowed: true}
	out, err := scope.Waterfall(g.scope, scope.Event[services.GuardDecision]{Name: "tool.before", Payload: decision}, decision)
	if err != nil {
		return services.ToolResult{}, err
	}
	if !out.Allowed {
		return services.ToolResult{}, services.ErrDenied
	}

	var result services.ToolResult
	err = guard.RunWithTimeout(ctx, defaultToolTimeout, func(ctx context.Context) error {
		var e error
		result, e = g.next.Execute(ctx, args)
		return e
	})
	if err != nil {
		return services.ToolResult{}, err
	}

	_ = scope.Emit(g.scope, scope.Event[services.GuardDecision]{Name: "tool.after", Payload: out})
	return result, nil
}
