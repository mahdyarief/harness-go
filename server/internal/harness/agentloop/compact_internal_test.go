package agentloop

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"hsg/internal/harness/core"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

func discard(services.AIEvent) error { return nil }

func TestCompactDropsOldTurnsUntilUnderCap(t *testing.T) {
	big := strings.Repeat("x", 4000)
	history := []ai.Message{{Role: "system", Content: "sys"}}
	for i := 0; i < 6; i++ {
		history = append(history,
			ai.Message{Role: "user", Content: big},
			ai.Message{Role: "assistant", Content: big})
	}
	cfg := services.LoopConfig{ContextTokenCap: 2500, KeepLastMessages: 2}
	n := 0
	out := (&defaultLoop{}).compact(context.Background(), history, nil, cfg, &n, discard)

	if len(out) >= len(history) {
		t.Fatalf("nothing dropped: %d -> %d", len(history), len(out))
	}
	if out[0].Role != "system" {
		t.Fatalf("system prompt lost: first role %q", out[0].Role)
	}
	if got := estimateTokens(out, nil); got > cfg.ContextTokenCap {
		t.Fatalf("estimate %d still above cap %d", got, cfg.ContextTokenCap)
	}
	if !reflect.DeepEqual(out[len(out)-1], history[len(history)-1]) {
		t.Fatal("latest message must survive compaction")
	}
}

func TestForceCompactKeepsOrderAndPairing(t *testing.T) {
	history := []ai.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "u0"}}
	for i := 0; i < 4; i++ {
		a, b := fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i)
		history = append(history,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: a}, {ID: b}}},
			ai.Message{Role: "tool", ToolCallID: a, Content: "r"},
			ai.Message{Role: "tool", ToolCallID: b, Content: "r"})
	}
	n := 0
	out := (&defaultLoop{}).forceCompact(context.Background(), history, nil, services.LoopConfig{}, 1, &n, discard)

	if len(out) >= len(history) {
		t.Fatalf("nothing dropped: %d -> %d", len(history), len(out))
	}
	if out[0].Role != "system" || out[1].Content != "u0" {
		t.Fatalf("head must stay system then first user, got %q %q", out[0].Role, out[1].Content)
	}
	if !reflect.DeepEqual(out[len(out)-1], history[len(history)-1]) {
		t.Fatal("latest message must stay last")
	}
	if ok, err := core.ToolPairingBalanced(out, len(out)); !ok || err != nil {
		t.Fatalf("pairing broken: ok=%v err=%v", ok, err)
	}
}

func TestForceCompactWithoutToolsKeepsLatestUserTurn(t *testing.T) {
	history := []ai.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "u0"}}
	for i := 1; i <= 4; i++ {
		history = append(history,
			ai.Message{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
			ai.Message{Role: "user", Content: fmt.Sprintf("u%d", i)})
	}
	n := 0
	out := (&defaultLoop{}).forceCompact(context.Background(), history, nil, services.LoopConfig{}, 1, &n, discard)
	if len(out) != 3 || out[0].Role != "system" || out[1].Content != "u0" || out[2].Content != "u4" {
		var got []string
		for _, m := range out {
			got = append(got, m.Role+":"+m.Content)
		}
		t.Fatalf("got %v; want [system u0 u4]", got)
	}
}

// DSH selectCompactableRange cuts at any tool-pairing-safe point, not only at
// user messages, so a single long tool chain can still be compacted. The user
// message that opened the turn is kept so the model does not lose the task.
func TestCompactCutsInsideASingleUserTurn(t *testing.T) {
	big := strings.Repeat("r", 6000) // below the 8192 prune threshold
	history := []ai.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "tugas"}}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("c%d", i)
		history = append(history,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: id}}},
			ai.Message{Role: "tool", ToolCallID: id, Content: big})
	}
	cfg := services.LoopConfig{ContextTokenCap: 3200, KeepLastMessages: 4}
	n := 0
	out := (&defaultLoop{}).compact(context.Background(), history, nil, cfg, &n, discard)

	if len(out) >= len(history) {
		t.Fatalf("nothing dropped: %d -> %d", len(history), len(out))
	}
	if got := estimateTokens(out, nil); got > cfg.ContextTokenCap {
		t.Fatalf("estimate %d above cap %d", got, cfg.ContextTokenCap)
	}
	if out[0].Role != "system" || out[1].Role != "user" || out[1].Content != "tugas" {
		t.Fatalf("head must be system then the turn's user message, got %q %q", out[0].Role, out[1].Content)
	}
	if ok, err := core.ToolPairingBalanced(out, len(out)); !ok || err != nil {
		t.Fatalf("pairing broken: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(out[len(out)-1], history[len(history)-1]) {
		t.Fatal("latest tool result must survive")
	}
}
