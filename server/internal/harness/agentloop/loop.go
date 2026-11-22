package agentloop

import (
	"context"
	"errors"
	"strings"
	"time"

	"hsg/internal/harness/guard"
	hsgllm "hsg/internal/harness/llm"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type defaultLoop struct {
	scope *scope.Scope
	now   func() time.Time
}

func New(s *scope.Scope) services.AgentLoop {
	return &defaultLoop{scope: s, now: time.Now}
}

const (
	nudgeNotice  = "Model berhenti setelah menggunakan alat tanpa menyelesaikan tugas. Mendorong untuk melanjutkan."
	nudgePrompt  = "Lanjutkan tugas dengan alat yang tersedia. Setelah benar-benar selesai, berikan ringkasan akhir yang singkat. Jika menunggu keputusan pengguna, ajukan pertanyaan."
	resumePrompt = "Lanjutkan jawaban dari titik terakhir tanpa mengulang. Selesaikan hingga selesai."
)

func (l *defaultLoop) Run(ctx context.Context, req services.RunRequest, sink services.Sink) (services.Outcome, error) {
	cfg, ok := scope.Require(l.scope, services.LoopConfigKey)
	if !ok {
		cfg = services.LoopConfig{}
	}
	cfg, err := ResolveLoopConfig(cfg)
	if err != nil {
		return services.Outcome{}, err
	}
	outerCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, cfg.MaxDuration)
	defer cancel()
	llm, ok := scope.Require(l.scope, services.LLMKey)
	if !ok {
		return services.Outcome{}, services.ErrLoopMissing
	}
	tools, _ := scope.Require(l.scope, services.ToolsKey)
	if sink == nil {
		sink = func(services.AIEvent) error { return nil }
	}

	started := l.now()
	out := services.Outcome{}
	history := append([]ai.Message(nil), req.Messages...)
	if req.SystemText != "" {
		history = append([]ai.Message{{Role: "system", Content: req.SystemText}}, history...)
	}
	repeatTool := guard.NewRepeatTool(8)
	reminder, reminderErr := guard.NewReminderTool(guard.ReminderConfig{
		Thresholds:            cfg.ReminderThresholds,
		Include:               cfg.ReminderInclude,
		Exclude:               cfg.ReminderExclude,
		ArgumentsPreviewChars: cfg.ReminderPreviewChars,
	})
	if reminderErr != nil {
		return services.Outcome{}, reminderErr
	}
	specs := toolSpecs(tools)
	guardMemory := make([]services.GuardMemory, len(cfg.LoopGuards))
	compacted := 0
	nudges := 0
	partialContinuations := 0
	toolCallsThisRun := 0
	overflowRetries := 0
	var carried strings.Builder
	// baseline is the last call's provider-reported usage; counted is the
	// history length it covers (through the assistant reply). Any compaction
	// invalidates it, since the counted prefix no longer exists.
	var baseline *ai.ChatResult
	counted := 0

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		before := compacted
		measured := estimateWithUsage(history, specs, baseline, counted)
		history = l.compactMeasured(ctx, history, specs, cfg, measured, &compacted, sink)
		if compacted != before {
			baseline = nil
		}

		// Only completion tokens are budgeted, as in the legacy runner: the
		// prompt is re-sent every turn and is already bounded by compaction.
		remaining := cfg.MaxTotalTokens - out.CompletionTokens
		if remaining <= 0 {
			_ = sink(services.AIEvent{Type: "budget", Error: services.ErrTokenBudget.Error()})
			out.Partial = true
			out.DurationMs = int(l.now().Sub(started).Milliseconds())
			return out, services.ErrTokenBudget
		}
		opts := ai.ChatOptions{Tools: specs, MaxTokens: remaining}
		if turn == 0 && cfg.ForceToolCall != "" && toolExists(specs, cfg.ForceToolCall) {
			opts.ToolChoice = cfg.ForceToolCall
		}

		result, billed, err := l.call(ctx, llm, history, opts, sink, cfg)
		out.PromptTokens += billed.prompt
		out.CompletionTokens += billed.completion
		if err != nil {
			if services.ErrorCode(err) == string(hsgllm.CodeContextWindow) &&
				overflowRetries < 1 && outerCtx.Err() == nil {
				overflowRetries++
				before := len(history)
				history = l.forceCompact(ctx, history, specs, cfg, max(1, cfg.ContextTokenCap/2), &compacted, sink)
				baseline = nil
				if len(history) < before {
					_ = sink(services.AIEvent{Type: "notice", Text: "Konteks terlalu panjang; riwayat dipangkas dan permintaan diulang."})
					continue
				}
			}
			// A provider-side stream cut leaves text the user already saw.
			// Resume it once per budget instead of returning a partial answer.
			// A stream that ends without [DONE] surfaces as ErrOutput, a
			// dropped connection as ErrProvider; both are resumable cuts.
			cut := result.Content != "" && (errors.Is(err, ai.ErrProvider) || errors.Is(err, ai.ErrOutput)) &&
				partialContinuations < cfg.MaxPartialContinuations && outerCtx.Err() == nil
			if cut {
				partialContinuations++
				carried.WriteString(result.Content)
				history = append(history,
					ai.Message{Role: "assistant", Content: result.Content},
					ai.Message{Role: "user", Content: resumePrompt})
				_ = sink(services.AIEvent{Type: "notice", Text: "Jawaban terputus; melanjutkan dari bagian terakhir."})
				continue
			}
			if outerErr := outerCtx.Err(); outerErr != nil {
				err = outerErr
			}
			if out.Content == "" && result.Content != "" {
				out.Content = carried.String() + result.Content
				out.Partial = true
			}
			out.Partial = true
			out.DurationMs = int(l.now().Sub(started).Milliseconds())
			return out, err
		}
		if result.FinishReason == "length" {
			out.Partial = true
		}
		out.Turns++
		overflowRetries = 0

		assistant := ai.Message{Role: "assistant", Content: result.Content, ToolCalls: result.ToolCalls}
		history = append(history, assistant)
		if result.PromptTokens > 0 {
			reported := result
			baseline, counted = &reported, len(history)
		}

		if len(result.ToolCalls) > 0 {
			toolCallsThisRun += len(result.ToolCalls)
			messages, records, refs, skipped, err := l.dispatch(ctx, tools, result.ToolCalls, turn, sink, cfg, repeatTool, &reminder, &out)
			out.References = append(out.References, refs...)
			out.Tools = append(out.Tools, records...)
			history = append(history, messages...)
			if err != nil {
				out.Partial = true
				return out, err
			}
			if skipped > 0 {
				_ = sink(services.AIEvent{Type: "budget", Error: services.ErrToolBudget.Error()})
				out.Partial = true
				out.DurationMs = int(l.now().Sub(started).Milliseconds())
				return out, services.ErrToolBudget
			}
			for i, g := range cfg.LoopGuards {
				if g == nil {
					continue
				}
				if guardMemory[i] == nil {
					guardMemory[i] = services.GuardMemory{}
				}
				verdict := g.Inspect(guardMemory[i], services.GuardTurn{
					// Records is this turn only: guards count into Memory, so
					// passing the whole history would recount old failures.
					Turn: turn, Records: records, History: out.Tools,
				})
				if verdict.Allowed() {
					continue
				}
				_ = sink(services.AIEvent{Type: "delta", Text: verdict.Reason})
				out.Content = verdict.Reason
				out.DurationMs = int(l.now().Sub(started).Milliseconds())
				return out, nil
			}
			continue
		}

		if toolCallsThisRun > 0 && nudges < cfg.MaxContinuationNudges &&
			shouldContinue(result.Content, nudges > 0) {
			if nudges == 0 {
				_ = sink(services.AIEvent{Type: "notice", Text: nudgeNotice})
			}
			nudges++
			history = append(history, ai.Message{Role: "user", Content: nudgePrompt})
			continue
		}
		if toolCallsThisRun > 0 && nudges >= cfg.MaxContinuationNudges && shouldContinue(result.Content, true) {
			out.Partial = true
		}
		out.Content = carried.String() + result.Content
		out.DurationMs = int(l.now().Sub(started).Milliseconds())
		return out, nil
	}

	_ = sink(services.AIEvent{Type: "budget", Error: services.ErrTurnBudget.Error()})
	out.Partial = true
	out.DurationMs = int(l.now().Sub(started).Milliseconds())
	return out, services.ErrTurnBudget
}

func toolSpecs(tools services.Tools) []ai.ToolSpec {
	if tools == nil {
		return nil
	}
	return tools.Specs()
}

func toolExists(specs []ai.ToolSpec, name string) bool {
	for _, s := range specs {
		if s.Function.Name == name {
			return true
		}
	}
	return false
}

// usage is the token count billed for one model call, summed over retries.
type usage struct{ prompt, completion int }

func (l *defaultLoop) call(ctx context.Context, model services.LLM, history []ai.Message, opts ai.ChatOptions, sink services.Sink, cfg services.LoopConfig) (ai.ChatResult, usage, error) {
	policy := hsgllm.DefaultRetryPolicy()
	policy.MaxRetries = cfg.MaxRetries
	// Usage is summed over every attempt so failed attempts are still billed.
	// result keeps the final attempt's own usage (the compaction baseline).
	var billed usage
	attempt := 0
	for {
		// The gateway returns usage only on error, so the text the user already
		// saw is captured here for resume and persistence.
		var streamed strings.Builder
		result, err := model.ChatStream(ctx, history, opts, func(d ai.StreamDelta) error {
			if d.Content == "" {
				return nil
			}
			streamed.WriteString(d.Content)
			return sink(services.AIEvent{Type: "delta", Text: d.Content})
		})
		billed.prompt += max(0, result.PromptTokens)
		billed.completion += max(0, result.CompletionTokens)
		emitted := streamed.Len() > 0
		if err != nil && emitted {
			result.Content = streamed.String()
		}
		// DSH EMPTY_RESPONSE: a finished stream with no text and no tool calls
		// is retried like a transient failure. Once retries are spent the empty
		// result is returned without an error, so the continuation policy
		// handles it as it always did.
		empty := err == nil && !emitted && result.Content == "" &&
			len(result.ToolCalls) == 0 && result.FinishReason != "length"
		retryErr := err
		if empty {
			retryErr = hsgllm.NewFailure("empty model response", string(hsgllm.CodeEmptyResponse), nil)
		}
		if (err == nil && !empty) || emitted || ctx.Err() != nil || !policy.Retryable(retryErr) || attempt >= cfg.MaxRetries {
			return result, billed, err
		}
		if sleepErr := policy.Sleep(ctx, policy.Delay(attempt+1)); sleepErr != nil {
			return result, billed, sleepErr
		}
		attempt++
	}
}
