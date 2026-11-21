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
	return l.compactTo(ctx, history, specs, cfg, cfg.ContextTokenCap, compacted, sink)
}

func (l *defaultLoop) compactTo(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, cap int, compacted *int, sink services.Sink) []ai.Message {
	if ctx.Err() != nil || estimateTokens(history, specs) <= cap {
		return history
	}
	return l.compactContext(ctx, history, specs, cfg, cap, compacted, sink)
}

// forceCompact ignores the local estimate: the provider already reported an
// overflow, so it must shed history even if the local meter disagrees. It keeps
// the system prompt, the first user message and the two most recent complete
// tool pairs, which always leaves a pairing-safe history.
func (l *defaultLoop) forceCompact(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, cap int, compacted *int, sink services.Sink) []ai.Message {
	if ctx.Err() != nil || len(history) <= 3 {
		return history
	}
	head := 0
	if history[0].Role == "system" {
		head = 1
	}
	out := append([]ai.Message(nil), history[:head+1]...)
	pairs := 0
	for i := len(history) - 1; i > head && pairs < 2; i-- {
		out = append([]ai.Message{history[i]}, out...)
		if history[i].Role == "tool" && i > head && len(history[i-1].ToolCalls) > 0 {
			out = append([]ai.Message{history[i-1]}, out...)
			pairs++
			i--
		}
	}
	if len(out) >= len(history) {
		return history
	}
	*compacted += len(history) - len(out)
	emitCompaction(sink, 0, len(history)-len(out))
	return out
}

func (l *defaultLoop) compactContext(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, cap int, compacted *int, sink services.Sink) []ai.Message {
	if ctx.Err() != nil {
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
	if count > 0 && estimateTokens(cleared, specs) <= cap {
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
		if estimateTokens(out, specs) <= cap || start <= head {
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

const (
	charsPerToken = 4
	roleOverhead  = 4
)

func estimateTokens(messages []ai.Message, specs []ai.ToolSpec) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content)/charsPerToken + roleOverhead
		for _, c := range m.ToolCalls {
			total += len(c.Function.Arguments)/charsPerToken + roleOverhead
		}
	}
	for _, s := range specs {
		total += len(s.Function.Parameters) / charsPerToken
	}
	return total
}

// estimateWithUsage uses the last call's reported token usage as the baseline
// and estimates only messages appended since counted, matching DSH's meter.
func estimateWithUsage(messages []ai.Message, specs []ai.ToolSpec, reported *ai.ChatResult, counted int) int {
	if reported == nil || counted <= 0 {
		return estimateTokens(messages, specs)
	}
	if counted > len(messages) {
		counted = len(messages)
	}
	baseline := reported.PromptTokens + reported.CompletionTokens
	return baseline + estimateTokens(messages[counted:], specs)
}
