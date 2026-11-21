package agentloop

import (
	"context"
	"fmt"

	"hsg/internal/harness/core"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

const compactKeepChars = 512

func (l *defaultLoop) compact(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, compacted *int, sink services.Sink) []ai.Message {
	if ctx.Err() != nil || estimateTokens(history, specs) <= cfg.ContextTokenCap {
		return history
	}
	head := 0
	if len(history) > 0 && history[0].Role == "system" {
		head = 1
	}
	keepFrom := max(head, len(history)-cfg.KeepLastMessages)
	cleared := append([]ai.Message(nil), history...)
	count := 0
	for i := head; i < keepFrom && i < len(cleared); i++ {
		if cleared[i].Role == "tool" && len([]rune(cleared[i].Content)) > 2*compactKeepChars {
			cleared[i].Content = core.RetainHeadTail(cleared[i].Content, compactKeepChars, compactKeepChars)
			count++
		}
	}
	if count > 0 && estimateTokens(cleared, specs) <= cfg.ContextTokenCap {
		*compacted += count
		emitCompaction(sink, count, 0)
		return cleared
	}
	start := keepFrom
	for start > head && start < len(cleared) && cleared[start].Role != "user" {
		start--
	}
	for {
		out := append([]ai.Message(nil), cleared[:min(start, len(cleared))]...)
		if start < len(cleared) {
			out = append(out, cleared[start:]...)
		}
		if ok, _ := core.ToolPairingBalanced(out, len(out)); !ok {
			if start > head {
				start--
				continue
			}
		}
		if estimateTokens(out, specs) <= cfg.ContextTokenCap || start <= head {
			dropped := len(history) - len(out)
			*compacted += dropped
			emitCompaction(sink, count, dropped)
			return out
		}
		next := start + 1
		for next < len(cleared) && cleared[next].Role != "user" {
			next++
		}
		if next >= len(cleared) {
			dropped := len(history) - len(out)
			*compacted += dropped
			emitCompaction(sink, count, dropped)
			return out
		}
		start = next
	}
}

func emitCompaction(sink services.Sink, pruned, dropped int) {
	if pruned == 0 && dropped == 0 {
		return
	}
	_ = sink(services.AIEvent{Type: "compaction", Text: fmt.Sprintf("pruned %d tool results, dropped %d messages", pruned, dropped)})
}

func estimateTokens(messages []ai.Message, specs []ai.ToolSpec) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
		for _, c := range m.ToolCalls {
			total += len(c.Function.Arguments) / 4
		}
	}
	for _, s := range specs {
		total += len(s.Function.Parameters) / 4
	}
	return total
}
