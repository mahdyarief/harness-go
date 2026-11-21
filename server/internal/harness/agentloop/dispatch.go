package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"hsg/internal/harness/guard"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
	"strings"
	"sync"
)

func (l *defaultLoop) dispatch(ctx context.Context, tools services.Tools, calls []ai.ToolCall, turn int, sink services.Sink, cfg services.LoopConfig, repeat *guard.RepeatTool, reminder *guard.ReminderTool, out *services.Outcome) ([]ai.Message, []services.ToolRecord, []services.Reference, int, error) {
	budget := max(0, cfg.MaxToolCalls-out.ToolCalls)
	results := make([]ai.Message, len(calls))
	records := make([]services.ToolRecord, len(calls))
	var refs []services.Reference
	limit := min(cfg.ToolResultBytes, cfg.MaxToolResultBytes)
	skipped := max(0, len(calls)-budget)

	type job struct {
		index int
		tool  services.Tool
		args  json.RawMessage
	}
	var jobs []job
	for i, call := range calls {
		if i >= budget {
			message := "Panggilan alat tidak dijalankan karena batas jumlah panggilan alat tercapai. Selesaikan jawaban dengan informasi yang ada."
			results[i] = toolError(call, message)
			records[i] = services.ToolRecord{Turn: turn, Name: call.Function.Name, Arguments: call.Function.Arguments, Result: message, Err: services.CodeToolNotStarted}
			continue
		}
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
				return nil, nil, nil, 0, err
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
		return results, records, refs, skipped, nil
	}
	out.ToolCalls += len(jobs)

	for _, j := range jobs {
		if err := sink(services.AIEvent{Type: "tool_start", ToolName: j.tool.Name(), ToolCall: calls[j.index].ID}); err != nil {
			return nil, nil, nil, 0, err
		}
	}

	var mu sync.Mutex
	sem := make(chan struct{}, cfg.MaxToolConcurrency)
	for start := 0; start < len(jobs); {
		end := start + 1
		readOnly := jobs[start].tool.ReadOnly()
		if readOnly {
			for end < len(jobs) && jobs[end].tool.ReadOnly() {
				end++
			}
		}
		group := jobs[start:end]
		var wg sync.WaitGroup
		for _, j := range group {
			wg.Add(1)
			go func(j job) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
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
		start = end
	}
	return results, records, refs, skipped, nil
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
