package tools

import (
	"encoding/json"
	"testing"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type schemaRegistry []json.RawMessage

func (r schemaRegistry) Lookup(string) (services.Tool, bool) { return nil, false }
func (r schemaRegistry) Specs() []ai.ToolSpec {
	out := make([]ai.ToolSpec, len(r))
	for i, raw := range r {
		out[i] = ai.ToolSpec{Type: "function", Function: ai.ToolFunction{Name: "tool", Parameters: raw}}
	}
	return out
}

func TestValidateSchemas(t *testing.T) {
	if err := ValidateSchemas(schemaRegistry{json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)}); err != nil {
		t.Fatalf("valid schema: %v", err)
	}
	if err := ValidateSchemas(schemaRegistry{json.RawMessage(`{"type":"array"}`)}); err == nil {
		t.Fatal("invalid schema accepted")
	}
}
