package handlers

import (
	"encoding/json"
	"testing"

	"hsg/internal/harness/schema"
)

type parameterized interface {
	Name() string
	Parameters() json.RawMessage
}

// Mount rejects a registry whose schemas cannot compile, so every schema the
// handlers publish must compile here first.
func TestToolSchemasCompile(t *testing.T) {
	tools := []parameterized{
		&listCategoriesTool{},
		&webSearchTool{},
		&searchKnowledgeTool{},
	}
	for _, tool := range tools {
		if _, err := schema.Compile(tool.Parameters()); err != nil {
			t.Fatalf("tool %q: %v", tool.Name(), err)
		}
	}
}
