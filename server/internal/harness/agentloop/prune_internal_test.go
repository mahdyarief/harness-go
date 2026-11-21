package agentloop

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// DSH compaction-tool-result-pruner defaults: threshold 8192, head 4096, tail 1024.
func TestCompactionPrunesOnlyToolResultsAboveDSHThreshold(t *testing.T) {
	large := strings.Repeat("a", 6000) + strings.Repeat("b", 4000)
	small := strings.Repeat("c", 8000)
	history := []ai.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "1"}, {ID: "2"}}},
		{Role: "tool", ToolCallID: "1", Content: large},
		{Role: "tool", ToolCallID: "2", Content: small},
		{Role: "user", Content: "lanjut"},
		{Role: "assistant", Content: "ok"},
	}
	cfg := services.LoopConfig{ContextTokenCap: 3800, KeepLastMessages: 2}
	n := 0
	out := (&defaultLoop{}).compact(context.Background(), history, nil, cfg, &n, discard)
	if len(out) != len(history) {
		t.Fatalf("prune alone fits the cap; nothing should be dropped (%d -> %d)", len(history), len(out))
	}
	pruned := out[3].Content
	head, rest, ok := strings.Cut(pruned, "[... tool result middle pruned ...]")
	if !ok || utf8.RuneCountInString(head) != 4096 || !strings.HasSuffix(rest, strings.Repeat("b", 1024)) {
		t.Fatalf("large result not pruned to 4096+1024 (head=%d runes)", utf8.RuneCountInString(head))
	}
	if strings.HasSuffix(rest, strings.Repeat("b", 1025)) {
		t.Fatal("tail kept more than 1024 runes")
	}
	if out[4].Content != small {
		t.Fatal("result under the 8192 threshold must stay intact")
	}
}
