package agentloop

import (
	"context"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

func (l *defaultLoop) compact(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, compacted *int) []ai.Message {
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
	for i := head; i < keepFrom; i++ {
		if cleared[i].Role == "tool" && cleared[i].Content != "" {
			cleared[i].Content = "[tool output cleared to fit context]"
			count++
		}
	}
	if count > 0 && estimateTokens(cleared, specs) <= cfg.ContextTokenCap {
		*compacted += count
		return cleared
	}
	start := keepFrom
	for start > head && cleared[start].Role != "user" {
		start--
	}
	for {
		out := append([]ai.Message(nil), cleared[:head]...)
		out = append(out, cleared[start:]...)
		if estimateTokens(out, specs) <= cfg.ContextTokenCap || start == head {
			*compacted += len(history) - len(out)
			return out
		}
		next := start + 1
		for next < len(cleared) && cleared[next].Role != "user" {
			next++
		}
		if next >= len(cleared) {
			*compacted += len(history) - len(out)
			return out
		}
		start = next
	}
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
