package agentloop

import (
	"encoding/json"
	"testing"

	"hsg/internal/lib/ai"
)

func testMessages(contents ...string) []ai.Message {
	out := make([]ai.Message, 0, len(contents))
	for _, c := range contents {
		out = append(out, ai.Message{Role: "user", Content: c})
	}
	return out
}

func TestEstimateTokensAddsRoleOverheadAndToolSchemas(t *testing.T) {
	messages := []ai.Message{
		{Role: "user", Content: "abcd"},
		{Role: "assistant", Content: "abcdefgh"},
	}
	spec := ai.ToolSpec{Type: "function", Function: ai.ToolFunction{
		Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`),
	}}
	want := 8 + 3 + len(`{"type":"object"}`)/4
	if got := estimateTokens(messages, []ai.ToolSpec{spec}); got != want {
		t.Fatalf("estimate=%d want %d", got, want)
	}
}

func TestEstimateTokensUsesReportedUsageBaseline(t *testing.T) {
	before := testMessages("aaaa")
	after := testMessages("aaaa", "bbbbbbbb")
	reported := &ai.ChatResult{PromptTokens: 100, CompletionTokens: 20}
	if got := estimateWithUsage(after, nil, reported, len(before)); got != 126 {
		t.Fatalf("baseline + appended = %d; want 126", got)
	}
	if got := estimateWithUsage(after, nil, nil, len(before)); got != 17 {
		t.Fatalf("no baseline = %d; want 17", got)
	}
}
