package tools

import (
	"context"
	"encoding/json"
	"strconv"

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
		svcRef := services.Reference{
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
			Meta:        referenceMeta(ref.Meta),
		}
		out.References = append(out.References, svcRef)
		// Every KB passage is DOCUMENT evidence with no data lost.
		out.Evidence = append(out.Evidence, services.DocumentItem(svcRef))
	}
	return out, nil
}

// referenceMeta flattens the tool's citation metadata into the string map the
// evidence layer stores. Scalars keep their text form; objects and arrays are
// compact JSON so no attribute is silently lost.
func referenceMeta(meta map[string]any) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for key, value := range meta {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if v != "" {
				out[key] = v
			}
		case bool:
			out[key] = strconv.FormatBool(v)
		case float64:
			out[key] = strconv.FormatFloat(v, 'g', -1, 64)
		case json.Number:
			out[key] = v.String()
		default:
			if encoded, err := json.Marshal(v); err == nil {
				out[key] = string(encoded)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
