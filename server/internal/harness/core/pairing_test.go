package core

import (
	"strings"
	"testing"

	"hsg/internal/lib/ai"
)

func TestToolPairingBalanced(t *testing.T) {
	history := []ai.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "1"}, {ID: "2"}}},
		{Role: "tool", Content: "a"},
		{Role: "tool", Content: "b"},
		{Role: "assistant", Content: "done"},
	}
	for _, cut := range []int{0, 1, 4, 5} {
		ok, err := ToolPairingBalanced(history, cut)
		if err != nil {
			t.Fatalf("cut=%d: %v", cut, err)
		}
		if !ok {
			t.Errorf("cut=%d must be balanced", cut)
		}
	}
	for _, cut := range []int{2, 3} {
		ok, err := ToolPairingBalanced(history, cut)
		if err != nil || ok {
			t.Fatalf("cut=%d: ok=%v err=%v; want unbalanced without error", cut, ok, err)
		}
	}
}

func TestToolPairingBalancedOrphanReturnsError(t *testing.T) {
	history := []ai.Message{{Role: "tool", Content: "orphan"}}
	_, err := ToolPairingBalanced(history, len(history))
	if err == nil || !strings.Contains(err.Error(), "orphan") {
		t.Fatalf("err=%v; want an orphan tool-result error", err)
	}
}
