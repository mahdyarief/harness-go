package agentloop

import (
	"context"
	"errors"
	"time"

	"hsg/internal/harness/guard"
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

func (l *defaultLoop) Run(ctx context.Context, req services.RunRequest, sink services.Sink) (services.Outcome, error) {
	cfg, ok := scope.Require(l.scope, services.LoopConfigKey)
	if !ok {
		cfg = services.LoopConfig{}
	}
	cfg, err := ResolveLoopConfig(cfg)
	if err != nil {
		return services.Outcome{}, err
	}
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
	compacted := 0

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		history = l.compact(ctx, history, specs, cfg, &compacted)

		opts := ai.ChatOptions{Tools: specs}
		if turn == 0 && cfg.ForceToolCall != "" && toolExists(specs, cfg.ForceToolCall) {
			opts.ToolChoice = cfg.ForceToolCall
		}

		result, err := l.call(ctx, llm, history, opts, sink, cfg)
		accumulate(&out, result)
		if err != nil {
			out.Partial = true
			return out, err
		}
		out.Turns++

		assistant := ai.Message{Role: "assistant", Content: result.Content, ToolCalls: result.ToolCalls}
		history = append(history, assistant)

		if len(result.ToolCalls) > 0 {
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
			continue
		}

		if shouldContinue(result.Content, out.Turns) && out.Turns < cfg.MaxTurns {
			history = append(history, ai.Message{Role: "user", Content: continuationPrompt})
			continue
		}
		out.Content = result.Content
		out.DurationMs = int(l.now().Sub(started).Milliseconds())
		return out, nil
	}

	out.Partial = true
	out.DurationMs = int(l.now().Sub(started).Milliseconds())
	return out, nil
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

func accumulate(out *services.Outcome, result ai.ChatResult) {
	if result.PromptTokens > 0 {
		out.PromptTokens += result.PromptTokens
	}
	if result.CompletionTokens > 0 {
		out.CompletionTokens += result.CompletionTokens
	}
}

func (l *defaultLoop) call(ctx context.Context, llm services.LLM, history []ai.Message, opts ai.ChatOptions, sink services.Sink, cfg services.LoopConfig) (ai.ChatResult, error) {
	var last ai.ChatResult
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		emitted := false
		result, err := llm.ChatStream(ctx, history, opts, func(d ai.StreamDelta) error {
			if d.Content == "" {
				return nil
			}
			emitted = true
			return sink(services.AIEvent{Type: "delta", Text: d.Content})
		})
		last = result
		if err == nil {
			return result, nil
		}
		lastErr = err
		var providerErr *ai.ProviderError
		if !errors.As(err, &providerErr) || !providerErr.Retryable || ctx.Err() != nil || emitted {
			return result, err
		}
		if attempt == cfg.MaxRetries {
			break
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 150 * time.Millisecond):
		}
	}
	return last, lastErr
}
