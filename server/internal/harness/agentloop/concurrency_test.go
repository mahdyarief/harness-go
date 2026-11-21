package agentloop_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

func lookupCall(id string) ai.ToolCall { return namedCall(id, "lookup") }

func namedCall(id, name string) ai.ToolCall {
	return ai.ToolCall{
		ID:       id,
		Type:     "function",
		Function: ai.ToolCallFunction{Name: name, Arguments: `{"q":"` + id + `"}`},
	}
}

type trackingTool struct {
	readOnly bool
	inFlight *int64
	peak     *int64
	delay    time.Duration
}

func (t *trackingTool) Name() string        { return "lookup" }
func (t *trackingTool) Description() string { return "alat uji" }
func (t *trackingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
}
func (t *trackingTool) ReadOnly() bool { return t.readOnly }
func (t *trackingTool) Execute(_ context.Context, _ json.RawMessage) (services.ToolResult, error) {
	cur := atomic.AddInt64(t.inFlight, 1)
	for {
		old := atomic.LoadInt64(t.peak)
		if cur <= old || atomic.CompareAndSwapInt64(t.peak, old, cur) {
			break
		}
	}
	time.Sleep(t.delay)
	atomic.AddInt64(t.inFlight, -1)
	return services.ToolResult{Output: "ok"}, nil
}

func TestConcurrencyBoundedAndWriteToolSerializes(t *testing.T) {
	var readInFlight, readPeak, writeInFlight, writePeak int64
	readTool := &trackingTool{readOnly: true, inFlight: &readInFlight, peak: &readPeak, delay: 20 * time.Millisecond}
	writeTool := &trackingTool{readOnly: false, inFlight: &writeInFlight, peak: &writePeak, delay: 20 * time.Millisecond}

	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(&recordingLLM{script: []ai.ChatResult{
		{FinishReason: "tool_calls", ToolCalls: []ai.ToolCall{
			lookupCall("a"), lookupCall("b"), namedCall("c", "write"), lookupCall("d"), lookupCall("e"),
		}},
	}}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(namespacedTools{read: readTool, write: writeTool}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{MaxToolConcurrency: 2}, nil); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var order []string
	_, err := agentloop.New(s).Run(context.Background(), services.RunRequest{Messages: userMsg("x")},
		func(e services.AIEvent) error {
			if e.Type == "tool_start" {
				mu.Lock()
				order = append(order, e.ToolCall)
				mu.Unlock()
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&readPeak); got > 2 {
		t.Fatalf("read peak in-flight=%d; want <= 2", got)
	}
	if got := atomic.LoadInt64(&writePeak); got != 1 {
		t.Fatalf("write peak in-flight=%d; want 1 (write tool must run alone)", got)
	}
	wantOrder := []string{"a", "b", "c", "d", "e"}
	for i, id := range wantOrder {
		if i >= len(order) || order[i] != id {
			t.Fatalf("tool_start order=%v want %v", order, wantOrder)
		}
	}
}
