package agent

import (
	"context"
	"encoding/json"
)

// Guards are the harness's deterministic safety checks. There are two shapes,
// and they attach at different seams:
//
//   - LoopGuard watches the run as a whole (for example "the same tool call
//     keeps failing") and can end it. It is registered on Config.LoopGuards and
//     consulted by Runner.Run once per turn, after the tools have executed.
//   - DomainGuard validates what the model proposed to one tool (for example a
//     coding suggestion that must match a known candidate list). It wraps a Tool
//     through Guarded and runs just before Execute.
//
// Both are pure decisions: no I/O, no clock, no logging, no goroutines. They
// get the data they need as arguments and return a Verdict, so they can be
// tested with plain values. The Runner owns every side effect that follows a
// verdict (events, history, stopping). Neither shape is a security boundary on
// its own: the allow-list and schema check in Registry stay the admission
// control, and guards only add judgement on top of calls that already passed it.

// Action is what a guard wants the harness to do.
type Action int

const (
	// ActionContinue lets the run or the call proceed. It is the zero value, so
	// a guard that has nothing to say needs no return value at all.
	ActionContinue Action = iota
	// ActionStop ends the run (LoopGuard) or rejects the single call
	// (DomainGuard).
	ActionStop
)

// Verdict is a guard's decision. Reason is written for the person reading the
// answer, in Indonesian like the rest of the user-facing text, and must not
// contain tool output, arguments, or provider errors.
type Verdict struct {
	Action Action
	Reason string
}

// Allow is the verdict for "nothing to flag".
func Allow() Verdict { return Verdict{} }

// Stop is the verdict for "end the run" or "reject this call".
func Stop(reason string) Verdict { return Verdict{Action: ActionStop, Reason: reason} }

// Allowed reports whether the guard let things proceed.
func (v Verdict) Allowed() bool { return v.Action == ActionContinue }

// Memory is the scratch space one guard gets for one run. The Runner creates it
// when the run starts and drops it when the run ends, so a guard instance holds
// no state of its own, is safe to share across concurrent runs, and cannot leak
// counts from one conversation into another. Each guard gets its own Memory, so
// keys never collide between guards.
type Memory map[string]int

// Incr adds one to key and returns the new count.
func (m Memory) Incr(key string) int {
	m[key]++
	return m[key]
}

// LoopTurn is what a LoopGuard sees after a turn's tools have run. The slices
// are shared with the Runner and must be treated as read-only.
type LoopTurn struct {
	// Turn is the zero-based index of the turn that just finished.
	Turn int
	// Records are the tool invocations of this turn, in the model's order.
	Records []ToolRecord
	// History holds every record of the run so far, including this turn's.
	History []ToolRecord
}

// LoopGuard decides, once per turn and after the tools have run, whether the
// run should go on. A Stop verdict ends the run gracefully: Reason becomes the
// final answer, the same way a model's own conclusion would, rather than the
// run failing with an error.
type LoopGuard interface {
	// Name identifies the guard in code and tests. It is not shown to users.
	Name() string
	// Inspect must be pure apart from writes to mem.
	Inspect(mem Memory, turn LoopTurn) Verdict
}

// DomainGuard reviews the arguments the model proposed to a tool, after they
// passed schema validation and before the tool runs.
type DomainGuard interface {
	// Name identifies the guard in the audit record and in tests.
	Name() string
	// Review must be pure. A Stop verdict rejects the call: the tool does not
	// run, and Reason is returned to the model as the tool result so it can
	// correct itself on the next turn.
	Review(tool string, args json.RawMessage) Verdict
}

// GuardRejection is the error a guarded tool returns when a DomainGuard
// refuses the call. The runner turns it into a recoverable tool result instead
// of the generic "tool execution failed".
type GuardRejection struct {
	Guard  string
	Reason string
}

func (e *GuardRejection) Error() string { return "rejected by guard " + e.Guard }

// Guarded wraps a tool so each DomainGuard reviews the call before Execute. The
// wrapped tool is unchanged: name, schema, and read-only flag pass through, so
// a guard can be added at registration time without touching the tool. Guards
// run in the order given and the first Stop wins. With no guards the tool is
// returned as is.
func Guarded(tool Tool, guards ...DomainGuard) Tool {
	var active []DomainGuard
	for _, g := range guards {
		if g != nil {
			active = append(active, g)
		}
	}
	if tool == nil || len(active) == 0 {
		return tool
	}
	return &guardedTool{Tool: tool, guards: active}
}

type guardedTool struct {
	Tool
	guards []DomainGuard
}

func (t *guardedTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	for _, g := range t.guards {
		if v := g.Review(t.Name(), args); !v.Allowed() {
			return ToolResult{}, &GuardRejection{Guard: g.Name(), Reason: v.Reason}
		}
	}
	return t.Tool.Execute(ctx, args)
}

// inspect runs the loop guards over one finished turn. It returns the first
// Stop verdict together with the name of the guard that issued it. A nil guard
// is skipped so a miswired slice cannot panic a run.
func inspect(guards []LoopGuard, memory []Memory, turn LoopTurn) (Verdict, string, bool) {
	for i, g := range guards {
		if g == nil {
			continue
		}
		if v := g.Inspect(memory[i], turn); !v.Allowed() {
			return v, g.Name(), true
		}
	}
	return Allow(), "", false
}
