package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
			messages, records, refs, err := l.dispatch(ctx, tools, result.ToolCalls, turn, sink, cfg, repeatTool, &reminder, &out)
			out.References = append(out.References, refs...)
			out.Tools = append(out.Tools, records...)
			if err != nil {
				out.Partial = true
				return out, err
			}
			history = append(history, messages...)
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

const continuationPrompt = "Lanjutkan pekerjaan yang belum selesai dari pesan Anda sebelumnya. Jangan ulangi bagian yang sudah Anda sampaikan."

func shouldContinue(text string, nudges int) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return true
	}
	if strings.HasSuffix(lower, "?") && nudges < 2 {
		return true
	}
	for _, phrase := range []string{
		"masih", "belum selesai", "lanjut", "akan saya", "saya akan", "selanjutnya",
		"mohon tunggu", "please wait", "let me", "i will", "i'll", "continuing", "still working",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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

func (l *defaultLoop) dispatch(ctx context.Context, tools services.Tools, calls []ai.ToolCall, turn int, sink services.Sink, cfg services.LoopConfig, repeat *guard.RepeatTool, reminder *guard.ReminderTool, out *services.Outcome) ([]ai.Message, []services.ToolRecord, []services.Reference, error) {
	if len(calls) > cfg.MaxToolCalls-out.ToolCalls {
		calls = calls[:max(0, cfg.MaxToolCalls-out.ToolCalls)]
	}
	results := make([]ai.Message, len(calls))
	records := make([]services.ToolRecord, len(calls))
	var refs []services.Reference
	limit := min(cfg.ToolResultBytes, cfg.MaxToolResultBytes)

	type job struct {
		index int
		tool  services.Tool
		args  json.RawMessage
	}
	var jobs []job
	for i, call := range calls {
		if call.Type != "" && call.Type != "function" {
			results[i] = toolError(call, "unsupported tool call type")
			records[i] = services.ToolRecord{Turn: turn, Name: call.Function.Name, Arguments: call.Function.Arguments, Err: "unsupported tool call type"}
			continue
		}
		tool, ok := tools.Lookup(call.Function.Name)
		if !ok {
			results[i] = toolError(call, "unknown tool "+call.Function.Name)
			records[i] = services.ToolRecord{Turn: turn, Name: call.Function.Name, Arguments: call.Function.Arguments, Err: "unknown tool"}
			continue
		}
		args := json.RawMessage(call.Function.Arguments)
		if err := validateArgs(tool.Parameters(), args); err != nil {
			results[i] = toolError(call, "arguments do not match schema")
			records[i] = services.ToolRecord{Turn: turn, Name: tool.Name(), Arguments: call.Function.Arguments, Err: "arguments do not match schema"}
			continue
		}
		if notice := reminder.Observe(tool.Name(), call.Function.Arguments); notice != nil {
			if err := sink(services.AIEvent{Type: "tool.reminder", Text: notice.Text, ToolName: tool.Name(), ToolCall: call.ID}); err != nil {
				return nil, nil, nil, err
			}
		}
		if decision := repeat.Check(tool.Name(), canonicalArgs(call.Function.Arguments)); decision.Flagged {
			results[i] = toolError(call, "identical tool call repeated; choose a different argument or finish the answer")
			records[i] = services.ToolRecord{Turn: turn, Name: tool.Name(), Arguments: call.Function.Arguments, Err: "repeated tool call"}
			continue
		}
		jobs = append(jobs, job{index: i, tool: tool, args: args})
	}
	if len(jobs) == 0 {
		return results, records, refs, nil
	}
	out.ToolCalls += len(jobs)

	for _, j := range jobs {
		if err := sink(services.AIEvent{Type: "tool_start", ToolName: j.tool.Name(), ToolCall: calls[j.index].ID}); err != nil {
			return nil, nil, nil, err
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			start := l.now()
			toolCtx, cancel := context.WithTimeout(ctx, cfg.ToolTimeout)
			decision := services.GuardDecision{Allowed: true}
			decision, gerr := scope.Waterfall(l.scope, scope.Event[services.GuardDecision]{Name: "tool.before", Payload: decision}, decision)
			var res services.ToolResult
			var err error
			if gerr == nil && !decision.Allowed {
				err = services.ErrDenied
			} else if gerr == nil {
				res, err = j.tool.Execute(toolCtx, j.args)
			} else {
				err = gerr
			}
			cancel()
			elapsed := int(l.now().Sub(start).Milliseconds())
			record := services.ToolRecord{Turn: turn, Name: j.tool.Name(), Arguments: calls[j.index].Function.Arguments, DurationMs: elapsed}
			if err != nil {
				message := "tool execution failed"
				if errors.Is(err, services.ErrDenied) {
					message = "tool denied: " + decision.Reason
					record.Err = "denied"
				} else if errors.Is(err, context.DeadlineExceeded) {
					timeoutErr := services.NewHarnessError("tool timed out", "TOOL_TIMEOUT", err)
					message = timeoutErr.Message
					record.Err = services.ErrorCode(timeoutErr)
				} else {
					record.Err = err.Error()
				}
				record.Result = message
				results[j.index] = toolError(calls[j.index], message)
				mu.Lock()
				records[j.index] = record
				mu.Unlock()
				return
			}
			output := truncate(res.Output, limit)
			results[j.index] = ai.Message{Role: "tool", ToolCallID: calls[j.index].ID, Content: output}
			record.Result = output
			record.References = res.References
			mu.Lock()
			records[j.index] = record
			refs = append(refs, res.References...)
			mu.Unlock()
			_ = scope.Emit(l.scope, scope.Event[services.GuardDecision]{Name: "tool.after", Payload: decision})
		}(j)
	}
	wg.Wait()
	return results, records, refs, nil
}

func toolError(call ai.ToolCall, message string) ai.Message {
	return ai.Message{Role: "tool", ToolCallID: call.ID, Content: message}
}

func validateArgs(schema, args json.RawMessage) error {
	var spec struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schema, &spec); err != nil || spec.Type != "object" {
		return ai.ErrInput
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(args, &input); err != nil || input == nil {
		return ai.ErrInput
	}
	for _, key := range spec.Required {
		if _, ok := input[key]; !ok {
			return ai.ErrInput
		}
	}
	for key := range input {
		if _, ok := spec.Properties[key]; !ok {
			return ai.ErrInput
		}
	}
	return nil
}

func canonicalArgs(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		if out, err := json.Marshal(v); err == nil {
			return string(out)
		}
	}
	return strings.Join(strings.Fields(raw), " ")
}

func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[truncated: result exceeded budget]"
}

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
