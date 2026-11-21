package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"hsg/internal/lib/ai"
)

// stopAfter is a stand-in LoopGuard: it counts turns in its Memory and stops
// once the limit is reached. It exists only to exercise the seam.
type stopAfter struct{ limit int }

func (g stopAfter) Name() string { return "stop-after" }
func (g stopAfter) Inspect(mem Memory, _ LoopTurn) Verdict {
	if mem.Incr("turns") >= g.limit {
		return Stop("dihentikan oleh guard")
	}
	return Allow()
}

// rejectArg is a stand-in DomainGuard that refuses any call whose arguments
// contain the marker.
type rejectArg struct{ marker string }

func (g rejectArg) Name() string { return "reject-arg" }
func (g rejectArg) Review(_ string, args json.RawMessage) Verdict {
	if strings.Contains(string(args), g.marker) {
		return Stop("usulan tidak valid, perbaiki lalu coba lagi")
	}
	return Allow()
}

func TestRunnerWithoutGuardsIsUnchanged(t *testing.T) {
	tool := &echoTool{name: "echo", read: true}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("c1", "echo", `{"q":"a"}`),
		{FinishReason: "stop", Content: "done", CompletionTokens: 5},
	}}
	out, err := newRunner(gateway, NewRegistry(tool), Config{}).Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil || out.Content != "done" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestLoopGuardStopEndsRunGracefully(t *testing.T) {
	tool := &echoTool{name: "echo", read: true}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("c1", "echo", `{"q":"a"}`),
		toolCallTurn("c2", "echo", `{"q":"b"}`),
		{FinishReason: "stop", Content: "never reached", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{LoopGuards: []LoopGuard{stopAfter{limit: 2}}})

	var deltas []string
	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, func(e Event) error {
		if e.Type == "delta" {
			deltas = append(deltas, e.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("a guard stop is a conclusion, not an error: %v", err)
	}
	if out.Content != "dihentikan oleh guard" || out.Turns != 2 || len(out.Tools) != 2 {
		t.Fatalf("unexpected outcome: %+v", out)
	}
	if len(deltas) != 1 || deltas[0] != out.Content {
		t.Fatalf("reason must reach the stream once: %v", deltas)
	}
	if calls := atomic.LoadInt32(&gateway.calls); calls != 2 {
		t.Fatalf("model called %d times, want 2", calls)
	}
}

func TestLoopGuardMemoryIsPerRun(t *testing.T) {
	tool := &echoTool{name: "echo", read: true}
	guard := stopAfter{limit: 2}
	for run := 0; run < 2; run++ {
		gateway := &fakeGateway{results: []ai.ChatResult{
			toolCallTurn("c1", "echo", `{"q":"a"}`),
			{FinishReason: "stop", Content: "done", CompletionTokens: 5},
		}}
		runner := newRunner(gateway, NewRegistry(tool), Config{LoopGuards: []LoopGuard{guard}})
		out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil)
		if err != nil || out.Content != "done" {
			t.Fatalf("run %d leaked guard state: out=%+v err=%v", run, out, err)
		}
	}
}

func TestNilLoopGuardIsSkipped(t *testing.T) {
	tool := &echoTool{name: "echo", read: true}
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("c1", "echo", `{"q":"a"}`),
		{FinishReason: "stop", Content: "done", CompletionTokens: 5},
	}}
	runner := newRunner(gateway, NewRegistry(tool), Config{LoopGuards: []LoopGuard{nil}})
	if out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil); err != nil || out.Content != "done" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestDomainGuardRejectsBeforeExecute(t *testing.T) {
	inner := &echoTool{name: "echo", read: true, output: "ok:"}
	guarded := Guarded(inner, rejectArg{marker: "BAD"})
	gateway := &fakeGateway{results: []ai.ChatResult{
		toolCallTurn("c1", "echo", `{"q":"BAD"}`),
		{FinishReason: "stop", Content: "done", CompletionTokens: 5},
	}}
	out, err := newRunner(gateway, NewRegistry(guarded), Config{}).Run(context.Background(), []ai.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil || out.Content != "done" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if atomic.LoadInt32(&inner.calls) != 0 {
		t.Fatal("tool ran despite a Stop verdict")
	}
	if len(out.Tools) != 1 || out.Tools[0].Err != "rejected by guard reject-arg" {
		t.Fatalf("audit record = %+v", out.Tools)
	}
	toolMsg := gateway.last[len(gateway.last)-1]
	if toolMsg.Role != "tool" || !strings.Contains(toolMsg.Content, "perbaiki lalu coba lagi") {
		t.Fatalf("model did not receive the reason: %+v", toolMsg)
	}
}

func TestDomainGuardAllowsCleanCall(t *testing.T) {
	inner := &echoTool{name: "echo", read: true, output: "ok:"}
	guarded := Guarded(inner, rejectArg{marker: "BAD"})
	got, err := guarded.Execute(context.Background(), json.RawMessage(`{"q":"fine"}`))
	if err != nil || got.Output != `ok:{"q":"fine"}` {
		t.Fatalf("got=%q err=%v", got.Output, err)
	}
}

func TestGuardedKeepsToolIdentity(t *testing.T) {
	inner := &echoTool{name: "echo", read: true}
	guarded := Guarded(inner, rejectArg{marker: "x"})
	if guarded.Name() != "echo" || !guarded.ReadOnly() || string(guarded.Parameters()) != string(inner.Parameters()) {
		t.Fatal("wrapper must not change name, schema, or read-only flag")
	}
	if Guarded(inner) != Tool(inner) || Guarded(inner, nil) != Tool(inner) {
		t.Fatal("no active guards must return the tool unchanged")
	}
	if Guarded(nil, rejectArg{}) != nil {
		t.Fatal("nil tool must stay nil")
	}
}
