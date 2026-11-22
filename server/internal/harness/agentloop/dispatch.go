package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"hsg/internal/harness/guard"
	"hsg/internal/harness/runlog"
	"hsg/internal/harness/schema"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
	"strings"
	"sync"
)

const (
	codeToolArgsInvalid   = "TOOL_ARGS_INVALID"
	codeToolSchemaInvalid = "TOOL_SCHEMA_INVALID"
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
		run   func(context.Context) (services.ToolResult, error)
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
		compiled, err := schema.Compile(tool.Parameters())
		if err != nil {
			message := "Skema argumen alat ini tidak valid; laporkan ke operator."
			results[i] = toolError(call, message)
			records[i] = services.ToolRecord{Turn: turn, Name: tool.Name(), Arguments: call.Function.Arguments, Result: message, Err: codeToolSchemaInvalid}
			continue
		}
		if violations := compiled.Validate(args); len(violations) > 0 {
			message := "Argumen alat tidak sesuai skema: " + describeViolations(violations) + ". Perbaiki lalu panggil ulang."
			results[i] = toolError(call, message)
			records[i] = services.ToolRecord{Turn: turn, Name: tool.Name(), Arguments: call.Function.Arguments, Result: message, Err: codeToolArgsInvalid}
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
		// DSH prepare phase: pipeline checks run before tool_start, so a
		// denied call is answered without ever being shown as started.
		run := func(ctx context.Context) (services.ToolResult, error) { return tool.Execute(ctx, args) }
		if p, ok := tool.(services.Preparer); ok {
			prepared, err := p.Prepare(ctx, args)
			if err != nil {
				message := err.Error()
				results[i] = toolError(call, message)
				records[i] = services.ToolRecord{Turn: turn, Name: tool.Name(), Arguments: call.Function.Arguments, Result: message, Err: services.ErrorCode(err)}
				continue
			}
			run = prepared
		}
		jobs = append(jobs, job{index: i, tool: tool, run: run})
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
				res, err := runBounded(toolCtx, j.run)
				cancel()
				elapsed := int(l.now().Sub(start).Milliseconds())
				record := services.ToolRecord{Turn: turn, Name: j.tool.Name(), Arguments: calls[j.index].Function.Arguments, DurationMs: elapsed}
				if err != nil {
					message := "tool execution failed"
					if errors.Is(err, context.DeadlineExceeded) {
						timeoutErr := services.NewHarnessError("tool timed out", "TOOL_TIMEOUT", err)
						message = timeoutErr.Message
						record.Err = services.ErrorCode(timeoutErr)
					} else if code := services.ErrorCode(err); code != "" {
						message = err.Error()
						record.Err = code
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
			}(j)
		}
		wg.Wait()
		// Record every finished tool in index order so the log is a
		// deterministic replay of the run; failures are recorded too.
		for _, j := range group {
			record := records[j.index]
			if record.Err != "" {
				continue
			}
			payload, err := json.Marshal(runlog.ToolResult{
				Name:       record.Name,
				ResultHash: runlog.HashResult(record.Result),
				Result:     runlog.Compact(record.Result),
				Evidence:   record.References,
			})
			if err != nil {
				continue
			}
			if err := sink(services.AIEvent{Type: runlog.EventTypeToolResult, Payload: payload}); err != nil {
				return nil, nil, nil, 0, err
			}
		}
		start = end
	}
	return results, records, refs, skipped, nil
}

func toolError(call ai.ToolCall, message string) ai.Message {
	return ai.Message{Role: "tool", ToolCallID: call.ID, Content: message}
}

func describeViolations(violations []schema.Violation) string {
	parts := make([]string, 0, len(violations))
	for _, v := range violations {
		parts = append(parts, v.Path+" ("+v.Reason+")")
	}
	return strings.Join(parts, ", ")
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

type toolOutcome struct {
	res services.ToolResult
	err error
}

// runBounded returns when the tool finishes or its context ends, whichever
// comes first. Cooperative tools stop on ctx as in DSH guard/timeout-policy;
// a tool that ignores ctx is left to finish in the background. Its late result
// lands in a buffered channel nobody reads, so it can never touch the loop's
// results, and ToolTimeout / MaxDuration stay hard bounds on the run.
func runBounded(ctx context.Context, run func(context.Context) (services.ToolResult, error)) (services.ToolResult, error) {
	done := make(chan toolOutcome, 1)
	go func() {
		res, err := run(ctx)
		done <- toolOutcome{res: res, err: err}
	}()
	select {
	case o := <-done:
		return o.res, o.err
	case <-ctx.Done():
		return services.ToolResult{}, ctx.Err()
	}
}
