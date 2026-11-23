// Package runlog adapts the harness's typed AI events to the append-only run
// event log. A Sink wraps a services.RunLog store and converts loop/team
// events into services.RunLogEvent rows, stamping the run and the producing
// member on every row.
//
// The adapter is deliberately event-driven: it attaches to the team and the
// loop through the existing services.Sink chain, so the loop's control flow is
// untouched. Tool, model and structured events carry their typed body in
// AIEvent.Payload; a plain tool_start maps to a tool/call row; everything
// else (delta, budget, notice, compaction) is not a durable run event and is
// skipped.
package runlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"hsg/internal/harness/services"
)

// Event type names. review/* is reserved for Task 7.
const (
	EventTypeRunStart       = "run/start"
	EventTypeRunEnd         = "run/end"
	EventTypeModelCall      = "model/call"
	EventTypeToolCall       = "tool/call"
	EventTypeToolResult     = "tool/result"
	EventTypeStructured     = "structured/submit"
	EventTypeReduceDecision = "reduce/decision"
	EventTypeReviewDecided  = "review/decided"
)

// ModelCall is the payload of a model/call event. The request hash is a stable
// digest of the request so the log stays safe by default; Prompt is included
// only when a LogPrompts option turns it on.
type ModelCall struct {
	Model            string `json:"model,omitempty"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	// CostUSD is set when the producer priced this call. It is a pointer so an
	// unpriced call is absent rather than a misleading zero.
	CostUSD      *float64 `json:"cost_usd,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
	LatencyMs    int      `json:"latency_ms"`
	RequestHash  string   `json:"request_hash"`
	Prompt       string   `json:"prompt,omitempty"`
}

// ToolCall is the payload of a tool/call event.
type ToolCall struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// ToolResult is the payload of a tool/result event. Result is a compact
// preview; ResultHash is the digest of the full output. Evidence preserves the
// services.Reference values.
type ToolResult struct {
	Name       string               `json:"name"`
	ResultHash string               `json:"result_hash"`
	Result     string               `json:"result"`
	Evidence   []services.Reference `json:"evidence,omitempty"`
}

// StructuredSubmit is the payload of a structured/submit event.
type StructuredSubmit struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments,omitempty"`
}

// ReduceDecision is the payload of a reduce/decision event; it records the
// winning rule of a team reduction.
type ReduceDecision struct {
	Rule    string `json:"rule"`
	Matched bool   `json:"matched"`
}

// ReviewDecided is the payload of a review/decided event. Result is a
// compact preview; the durable decision lives in review_decisions.
type ReviewDecided struct {
	ItemID   string `json:"item_id"`
	Subject  string `json:"subject"`
	Reviewer string `json:"reviewer"`
	Role     string `json:"role"`
	Reason   string `json:"reason,omitempty"`
	Result   string `json:"result,omitempty"`
}

// LogPrompts controls whether model/call events embed prompt text.
type LogPrompts struct {
	Enabled bool
}

// SinkOpts tunes the Sink.
type SinkOpts struct {
	Prompts LogPrompts
}

// Sink converts harness AI events into RunLogEvent rows on the given store.
// Store must be non-nil; RunID and Member are stamped on every row, with the
// event's own Agent taking precedence when set (a team sets it per member).
// ctx must carry the caller's tenant (lib.WithTenant) so the repository can
// pin app.tenant_id for the append.
func Sink(ctx context.Context, store services.RunLog, runID, member string, opts ...SinkOpts) services.Sink {
	if store == nil {
		panic("runlog: store must not be nil")
	}
	prompts := LogPrompts{}
	if len(opts) > 0 {
		prompts = opts[0].Prompts
	}
	return func(event services.AIEvent) error {
		row, ok := translate(event, prompts)
		if !ok {
			return nil
		}
		row.RunID = runID
		if event.Agent != "" {
			row.Member = event.Agent
		} else {
			row.Member = member
		}
		return store.AppendEvent(ctx, row)
	}
}

// translate maps one AI event to a durable run-log row. The boolean result is
// false when the event is not a run-log event (transient deltas, budget and
// notice signals, compaction).
func translate(event services.AIEvent, prompts LogPrompts) (services.RunLogEvent, bool) {
	// A caller that attached a typed body (model/call, tool/result,
	// structured/submit, reduce/decision) passes it straight through.
	if len(event.Payload) > 0 {
		typeName := event.Type
		if typeName == "" {
			typeName = "ai/event"
		}
		if typeName == EventTypeModelCall && !prompts.Enabled {
			// Drop prompt text so the default log stays safe.
			strip := struct {
				Model            string   `json:"model,omitempty"`
				PromptTokens     int      `json:"prompt_tokens"`
				CompletionTokens int      `json:"completion_tokens"`
				CostUSD          *float64 `json:"cost_usd,omitempty"`
				FinishReason     string   `json:"finish_reason,omitempty"`
				LatencyMs        int      `json:"latency_ms"`
				RequestHash      string   `json:"request_hash"`
				Prompt           string   `json:"prompt,omitempty"`
			}{}
			_ = json.Unmarshal(event.Payload, &strip)
			strip.Prompt = ""
			safe, _ := json.Marshal(strip)
			return services.RunLogEvent{Type: typeName, Payload: safe}, true
		}
		return services.RunLogEvent{Type: typeName, Payload: event.Payload}, true
	}

	switch event.Type {
	case "tool_start":
		payload, _ := json.Marshal(ToolCall{Name: event.ToolName, Args: event.ToolCall})
		return services.RunLogEvent{Type: EventTypeToolCall, Payload: payload}, true
	case EventTypeRunStart:
		return services.RunLogEvent{Type: EventTypeRunStart, Payload: json.RawMessage("null")}, true
	case EventTypeRunEnd:
		payload, _ := json.Marshal(map[string]string{"status": statusOf(event)})
		return services.RunLogEvent{Type: EventTypeRunEnd, Payload: payload}, true
	default:
		return services.RunLogEvent{}, false
	}
}

func statusOf(event services.AIEvent) string {
	if event.Error != "" {
		return event.Error
	}
	return "ok"
}

// RunStart writes a run/start event for a run.
func RunStart(store services.RunLog, runID, member string) error {
	return store.AppendEvent(context.Background(), services.RunLogEvent{
		RunID: runID, Member: member, Type: EventTypeRunStart,
	})
}

// RunEnd writes a run/end event carrying the terminal status.
func RunEnd(store services.RunLog, runID, member, status string) error {
	payload, _ := json.Marshal(map[string]string{"status": status})
	return store.AppendEvent(context.Background(), services.RunLogEvent{
		RunID: runID, Member: member, Type: EventTypeRunEnd, Payload: payload,
	})
}

// NewModelCallEvent builds a model/call row. When prompts is enabled and the
// call carries a Prompt it is embedded; otherwise only the request hash.
func NewModelCallEvent(runID, member string, call ModelCall, prompts LogPrompts) services.RunLogEvent {
	if call.RequestHash == "" {
		call.RequestHash = HashRequest(call.Prompt)
	}
	if !prompts.Enabled {
		call.Prompt = ""
	}
	payload, _ := json.Marshal(call)
	return services.RunLogEvent{RunID: runID, Member: member, Type: EventTypeModelCall, Payload: payload}
}

// NewToolCallEvent builds a tool/call row.
func NewToolCallEvent(runID, member, name, args string) services.RunLogEvent {
	payload, _ := json.Marshal(ToolCall{Name: name, Args: args})
	return services.RunLogEvent{RunID: runID, Member: member, Type: EventTypeToolCall, Payload: payload}
}

// NewToolResultEvent builds a tool/result row with a compact preview and the
// digest of the full output.
func NewToolResultEvent(runID, member, name, output string, refs []services.Reference) services.RunLogEvent {
	payload, _ := json.Marshal(ToolResult{
		Name:       name,
		ResultHash: HashResult(output),
		Result:     Compact(output),
		Evidence:   refs,
	})
	return services.RunLogEvent{RunID: runID, Member: member, Type: EventTypeToolResult, Payload: payload}
}

// StructuredEvent builds a structured/submit row.
func StructuredEvent(runID, member, tool string, args json.RawMessage) services.RunLogEvent {
	payload, _ := json.Marshal(StructuredSubmit{Tool: tool, Arguments: string(args)})
	return services.RunLogEvent{RunID: runID, Member: member, Type: EventTypeStructured, Payload: payload}
}

// ReduceDecisionEvent builds a reduce/decision row.
func ReduceDecisionEvent(runID, member string, decision ReduceDecision) services.RunLogEvent {
	payload, _ := json.Marshal(decision)
	return services.RunLogEvent{RunID: runID, Member: member, Type: EventTypeReduceDecision, Payload: payload}
}

// ReviewDecidedEvent writes a review/decided row for one inbox decision.
func ReviewDecidedEvent(runID, member string, event ReviewDecided) services.RunLogEvent {
	payload, _ := json.Marshal(event)
	return services.RunLogEvent{RunID: runID, Member: member, Type: EventTypeReviewDecided, Payload: payload}
}

// HashRequest returns a stable hex digest of a request body, safe by default.
func HashRequest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// HashResult returns a stable hex digest of a tool result.
func HashResult(result string) string {
	sum := sha256.Sum256([]byte(result))
	return hex.EncodeToString(sum[:])
}

// Compact truncates a tool result to a short, human-safe preview.
func Compact(result string) string {
	const limit = 256
	if len(result) <= limit {
		return result
	}
	return result[:limit] + "…"
}
