package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"hsg/internal/harness/tools"
	"hsg/internal/lib/agent"
)

type citedTool struct{ meta map[string]any }

func (t citedTool) Name() string                { return "cited" }
func (t citedTool) Description() string         { return "kutipan" }
func (t citedTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t citedTool) ReadOnly() bool              { return true }
func (t citedTool) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{References: []agent.Reference{{
		DocumentID: uuid.New(),
		ChunkID:    uuid.New(),
		Title:      "Dokumen",
		Meta:       t.meta,
	}}}, nil
}

func TestAdaptPreservesCitationMetadata(t *testing.T) {
	registry := agent.NewRegistry(citedTool{meta: map[string]any{
		"version": "v2",
		"tier":    "gold",
		"score":   json.Number("3"),
		"active":  true,
		"nested":  map[string]any{"k": "v"},
	}})
	tool, ok := tools.Adapt(registry).Lookup("cited")
	if !ok {
		t.Fatal("tool tidak ditemukan")
	}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.References) != 1 {
		t.Fatalf("referensi = %d, mau 1", len(res.References))
	}
	meta := res.References[0].Meta
	want := map[string]string{
		"version": "v2",
		"tier":    "gold",
		"score":   "3",
		"active":  "true",
		"nested":  `{"k":"v"}`,
	}
	for key, value := range want {
		if meta[key] != value {
			t.Errorf("meta[%q] = %q, mau %q", key, meta[key], value)
		}
	}
	if len(res.Evidence) != 1 {
		t.Fatalf("evidence = %d, mau 1", len(res.Evidence))
	}
	if got := res.Evidence[0].Meta["version"]; got != "v2" {
		t.Errorf("evidence version = %q, mau v2", got)
	}
}
