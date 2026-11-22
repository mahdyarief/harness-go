package tools

import (
	"context"
	"encoding/json"

	"hsg/internal/harness/services"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type adaptedRegistry struct{ registry *agent.Registry }

type adaptedTool struct{ tool agent.Tool }

func Adapt(r *agent.Registry) services.Tools {
	if r == nil {
		r = agent.NewRegistry()
	}
	return &adaptedRegistry{registry: r}
}

func (a *adaptedRegistry) Lookup(name string) (services.Tool, bool) {
	t, ok := a.registry.Lookup(name)
	if !ok {
		return nil, false
	}
	return &adaptedTool{tool: t}, true
}

func (a *adaptedRegistry) Specs() []ai.ToolSpec { return a.registry.Specs() }

func (t *adaptedTool) Name() string                { return t.tool.Name() }
func (t *adaptedTool) Description() string         { return t.tool.Description() }
func (t *adaptedTool) Parameters() json.RawMessage { return t.tool.Parameters() }
func (t *adaptedTool) ReadOnly() bool              { return t.tool.ReadOnly() }
func (t *adaptedTool) Terminal() bool {
	terminal, ok := t.tool.(interface{ Terminal() bool })
	return ok && terminal.Terminal()
}

func (t *adaptedTool) Execute(ctx context.Context, args json.RawMessage) (services.ToolResult, error) {
	res, err := t.tool.Execute(ctx, args)
	if err != nil {
		return services.ToolResult{}, err
	}
	out := services.ToolResult{Output: res.Output}
	for _, ref := range res.References {
		out.References = append(out.References, services.Reference{
			DocumentID:  ref.DocumentID.String(),
			ChunkID:     ref.ChunkID.String(),
			Title:       ref.Title,
			Filename:    ref.Filename,
			Format:      ref.Format,
			Locator:     ref.Locator,
			HeadingPath: ref.HeadingPath,
			Position:    ref.Position,
			Excerpt:     ref.Excerpt,
			Source:      ref.Source,
		})
	}
	return out, nil
}
