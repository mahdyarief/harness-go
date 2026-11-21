package agent

import (
	"context"
	"testing"

	"hsg/internal/lib/ai"
)

func failed(name, args, err string) ToolRecord {
	return ToolRecord{Name: name, Arguments: args, Err: err}
}

func TestRepeatedErrorSignature(t *testing.T) {
	if errorSignature(ToolRecord{Name: "a", Arguments: `{"q":"x"}`}) != "" {
		t.Fatal("a successful call must have no signature")
	}
	a := errorSignature(failed("a", `{"q": "x", "n": 1}`, "tool execution failed"))
	b := errorSignature(failed("a", `{"n":1,"q":"x"}`, "tool execution failed"))
	if a == "" || a != b {
		t.Fatalf("key order and whitespace must not change the signature: %q vs %q", a, b)
	}
	for name, other := range map[string]ToolRecord{
		"tool":      failed("b", `{"q":"x","n":1}`, "tool execution failed"),
		"arguments": failed("a", `{"q":"y","n":1}`, "tool execution failed"),
		"error":     failed("a", `{"q":"x","n":1}`, "tool timed out"),
	} {
		if errorSignature(other) == a {
			t.Fatalf("a different %s must produce a different signature", name)
		}
	}
	m1 := errorSignature(failed("a", "{not json", "e"))
	m2 := errorSignature(failed("a", "{not   json", "e"))
	if m1 == "" || m1 != m2 {
		t.Fatal("malformed arguments must still match themselves")
	}
}

func TestRepeatedErrorGuardInspect(t *testing.T) {
	guard := NewRepeatedErrorGuard()
	mem := Memory{}
	same := LoopTurn{Records: []ToolRecord{failed("a", `{"q":"x"}`, "tool execution failed")}}

	if v := guard.Inspect(mem, same); !v.Allowed() {
		t.Fatal("the first failure must give the model a chance to recover")
	}
	if v := guard.Inspect(mem, LoopTurn{Records: []ToolRecord{{Name: "a", Arguments: `{"q":"x"}`}}}); !v.Allowed() {
		t.Fatal("a success must not count")
	}
	v := guard.Inspect(mem, same)
	if v.Allowed() || v.Reason != repeatedErrorAnswer {
		t.Fatalf("the second identical failure must stop the run: %+v", v)
	}
}

func TestRepeatedErrorGuardIgnoresDifferentFailures(t *testing.T) {
	guard := NewRepeatedErrorGuard()
	mem := Memory{}
	for _, q := range []string{"a", "b", "c", "d"} {
		turn := LoopTurn{Records: []ToolRecord{failed("t", `{"q":"`+q+`"}`, "tool execution failed")}}
		if v := guard.Inspect(mem, turn); !v.Allowed() {
			t.Fatalf("different inputs are not a loop (q=%s)", q)
		}
	}
}

func TestRepeatedErrorGuardCountsWithinOneTurn(t *testing.T) {
	guard := NewRepeatedErrorGuard()
	call := failed("a", `{"q":"x"}`, "tool timed out")
	if v := guard.Inspect(Memory{}, LoopTurn{Records: []ToolRecord{call, call}}); v.Allowed() {
		t.Fatal("two identical failures in one turn must stop the run")
	}
}

func TestRepeatedErrorGuardLimit(t *testing.T) {
	call := failed("a", `{"q":"x"}`, "tool timed out")
	turn := LoopTurn{Records: []ToolRecord{call}}

	three := NewRepeatedErrorGuard().WithLimit(3)
	mem := Memory{}
	if !three.Inspect(mem, turn).Allowed() || !three.Inspect(mem, turn).Allowed() {
		t.Fatal("limit 3 must tolerate two failures")
	}
	if three.Inspect(mem, turn).Allowed() {
		t.Fatal("limit 3 must stop on the third")
	}

	low := NewRepeatedErrorGuard().WithLimit(1)
	mem = Memory{}
	if !low.Inspect(mem, turn).Allowed() {
		t.Fatal("a limit below 2 must be raised to 2")
	}
}

func TestRepeatedErrorGuardEndsRealRun(t *testing.T) {
	tool := &echoTool{name: "echo", read: true, fail: true}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("c1", "echo", `{"q":"a"}`),
		toolCallTurn("c2", "echo", `{"q":"a"}`),
		{FinishReason: "stop", Content: "never reached", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{LoopGuards: []LoopGuard{NewRepeatedErrorGuard()}})

	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("a guard stop is a conclusion, not an error: %v", err)
	}
	if out.Content != repeatedErrorAnswer || out.Turns != 2 {
		t.Fatalf("unexpected outcome: %+v", out)
	}
	if calls := int(gateway.calls); calls != 2 {
		t.Fatalf("model called %d times, want 2", calls)
	}
}

func TestRepeatedErrorGuardStopsModelIgnoringDomainGuard(t *testing.T) {
	inner := &echoTool{name: "echo", read: true}
	guarded := Guarded(inner, rejectArg{marker: "BAD"})
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("c1", "echo", `{"q":"BAD"}`),
		toolCallTurn("c2", "echo", `{"q":"BAD"}`),
		{FinishReason: "stop", Content: "never reached", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(guarded), Config{LoopGuards: []LoopGuard{NewRepeatedErrorGuard()}})

	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil || out.Content != repeatedErrorAnswer {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if inner.calls != 0 {
		t.Fatal("the rejected tool must never have run")
	}
}
