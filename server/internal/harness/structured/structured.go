// Package structured builds generic terminal tools: a tool whose successful
// call delivers validated JSON as the run's result.
package structured

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hsg/internal/harness/schema"
	"hsg/internal/harness/services"
)

// Submit returns a terminal tool named name whose arguments must satisfy
// schema. Execute echoes the validated arguments back as its output, which the
// loop places in Outcome.Structured.
func Submit(name, description string, raw json.RawMessage) services.Tool {
	return submitTool{name: name, description: description, raw: raw}
}

type submitTool struct {
	name        string
	description string
	raw         json.RawMessage
}

func (t submitTool) Name() string                { return t.name }
func (t submitTool) Description() string         { return t.description }
func (t submitTool) Parameters() json.RawMessage { return t.raw }
func (t submitTool) ReadOnly() bool              { return true }
func (t submitTool) Terminal() bool              { return true }

func (t submitTool) Execute(_ context.Context, args json.RawMessage) (services.ToolResult, error) {
	compiled, err := schema.Compile(t.raw)
	if err != nil {
		return services.ToolResult{}, fmt.Errorf("invalid tool schema: %w", err)
	}
	if violations := compiled.Validate(args); len(violations) > 0 {
		parts := make([]string, 0, len(violations))
		for _, v := range violations {
			parts = append(parts, v.Path+" ("+v.Reason+")")
		}
		return services.ToolResult{}, fmt.Errorf("arguments do not match schema: %s", strings.Join(parts, ", "))
	}
	return services.ToolResult{Output: string(args)}, nil
}
