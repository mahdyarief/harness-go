package agentloop

import (
	"context"
	"fmt"
	"unicode/utf8"

	"hsg/internal/harness/core"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// Tool-result pruning defaults, ported from DSH
// compaction-tool-result-pruner/src/config.ts:11-13.
const (
	pruneThresholdChars = 8192
	pruneHeadChars      = 4096
	pruneTailChars      = 1024
)

func (l *defaultLoop) compact(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, compacted *int, sink services.Sink) []ai.Message {
	return l.compactMeasured(ctx, history, specs, cfg, estimateTokens(history, specs), compacted, sink)
}

// compactMeasured compacts when measured (the usage-baselined size of history)
// exceeds the cap. Re-measurement after pruning uses the local estimate, since
// the provider baseline no longer describes the edited history.
func (l *defaultLoop) compactMeasured(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, measured int, compacted *int, sink services.Sink) []ai.Message {
	if ctx.Err() != nil || measured <= cfg.ContextTokenCap {
		return history
	}
	return l.compactContext(ctx, history, specs, cfg, cfg.ContextTokenCap, compacted, sink)
}

// forceCompact ignores the local estimate: the provider already reported an
// overflow, so it must shed history even if the local meter disagrees. It keeps,
// in order, the system prompt, the first user message, and a suffix that starts
// at the second most recent tool-calling assistant message (or, with fewer
// than two, at the latest user message). A suffix starting at an assistant or
// user message always carries every tool result it needs, so pairing holds.
func (l *defaultLoop) forceCompact(ctx context.Context, history []ai.Message, specs []ai.ToolSpec, cfg services.LoopConfig, cap int, compacted *int, sink services.Sink) []ai.Message {
	if ctx.Err() != nil || len(history) <= 3 {
		return history
	}
	keepHead := 1
	if history[0].Role == "system" {
		keepHead = 2
	}
	start, lastUser, groups := -1, -1, 0
	for i := len(history) - 1; i >= keepHead; i-- {
		msg := history[i]
		if msg.Role == "user" && lastUser < 0 {
			lastUser = i
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			groups++
			if groups == 2 {
				start = i
				break
			}
		}
	}
	if start < 0 {
		start = lastUser
	}
	if start <= keepHead {
		return history
	}
	out := append(append([]ai.Message(nil), history[:keepHead]...), history[start:]...)
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
		if cleared[i].Role == "tool" && utf8.RuneCountInString(cleared[i].Content) > pruneThresholdChars {
			cleared[i].Content = core.RetainHeadTail(cleared[i].Content, pruneHeadChars, pruneTailChars)
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
		out := append([]ai.Message(nil), cleared[:head]...)
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
	// Tool schemas are already inside the reported prompt tokens.
	baseline := reported.PromptTokens + reported.CompletionTokens
	return baseline + estimateTokens(messages[counted:], nil)
}
