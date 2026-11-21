package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// A domain guard is a pure check on what the model proposed to ONE tool. Use it
// when a tool has a rule that is cheap to verify deterministically and costly
// to get wrong: a suggested code that must come from a known candidate list, a
// value that must stay within a range, an identifier that must belong to the
// caller's tenant.
//
// How to add one, in order:
//
//  1. Implement DomainGuard. Review gets the tool name and the raw JSON
//     arguments, which already passed the tool's schema check. It returns
//     Allow() or Stop(reason). Keep it pure: no database, no HTTP, no clock. If
//     the rule needs data (a candidate list, the caller's tenant), capture it in
//     the guard's fields when you construct it, per request.
//  2. Write the reason for the model, not the user. It is returned as the tool
//     result so the model can fix the call on its next turn. Say what is wrong
//     and what would be accepted, and never echo secrets or other tenants' data.
//  3. Wrap the tool where the registry is built (handlers/chat.go toolRegistry):
//     agent.Guarded(tool, guard). The tool itself stays untouched.
//  4. Keep RepeatedErrorGuard registered. It counts guard rejections like any
//     other failure, so a model that keeps resubmitting the rejected call is
//     stopped instead of looping to the turn limit.
//
// A guard narrows what a call may do; it is not an authorization check.
// Tenant and role enforcement stay in the repositories, where the row-level
// security policies live.

// capGuard is a minimal DomainGuard used by the example below: it rejects a
// limit argument above a fixed ceiling.
type capGuard struct{ max float64 }

func (g capGuard) Name() string { return "limit-cap" }

func (g capGuard) Review(_ string, args json.RawMessage) Verdict {
	var in struct {
		Limit float64 `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return Allow() // malformed input is the schema check's job, not this guard's
	}
	if in.Limit > g.max {
		return Stop(fmt.Sprintf("limit terlalu besar, maksimum %.0f", g.max))
	}
	return Allow()
}

type listTool struct{}

func (listTool) Name() string        { return "list_items" }
func (listTool) Description() string { return "lists items" }
func (listTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"number"}},"required":["limit"]}`)
}
func (listTool) ReadOnly() bool { return true }
func (listTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{Output: "ok"}, nil
}

func ExampleGuarded() {
	tool := Guarded(listTool{}, capGuard{max: 50})

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"limit":500}`))
	var rejection *GuardRejection
	if errors.As(err, &rejection) {
		fmt.Println(rejection.Guard + ": " + rejection.Reason)
	}

	out, _ := tool.Execute(context.Background(), json.RawMessage(`{"limit":10}`))
	fmt.Println(out.Output)

	// Output:
	// limit-cap: limit terlalu besar, maksimum 50
	// ok
}
